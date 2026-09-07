package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

var (
	flagInitForce bool
	flagInitPrint bool
)

func newInitCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "init",
		Short: "Create .secretman.yaml, then create the environments it declares",
		Long: `Gets a repository into a rotatable state, writing no secret value.

Two things happen, in order.

  1. The config. init looks for .secretman.yaml, walking up from the working
     directory. If there is none, it asks for the project, the stores, the
     environments and the secrets, and writes the file at the repository root.
     If there is one, it stops and asks before touching it.

  2. The environments. The GitHub Environments named in the config are created
     if missing. An environment-scoped secret cannot be written until its
     environment exists, so this has to happen before the first rotation.

Overwriting an existing config DETACHES it: nothing is deleted from any store,
but a secret dropped from the file stops being rotated, verified or listed by
this tool, and nothing will mention it again. To change one secret rather than
all of them, use config add, config edit or config rm.

The file it writes holds names, kinds and validation rules. It never holds a
value or a digest of one, and is meant to be committed.`,
		Example: `  # First run in a fresh repository: wizard, then create the environments
  secretman init

  # Look at the config it would write, without writing it
  secretman init --print

  # Replace an existing config without being asked to confirm
  secretman init --force

  # Create the environments for a config that already exists, and stop
  secretman doctor && secretman init --force --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.Context())
		},
	}
	c.Flags().BoolVarP(&flagInitForce, "force", "f", false,
		"replace an existing config without asking (detaches it)")
	c.Flags().BoolVar(&flagInitPrint, "print", false,
		"print the config to stdout instead of writing it")
	return c
}

func runInit(ctx context.Context) error {
	path, existing, err := initTarget()
	if err != nil {
		return err
	}

	if existing {
		ui.Title("secretman › init")
		ui.Note("config %s", path)
		ok, err := confirmOverwrite(path)
		if err != nil {
			return err
		}
		if !ok {
			ui.Blank()
			ui.Note("Nothing written. To change one secret rather than all of them:")
			ui.Note("  secretman config add          add a secret")
			ui.Note("  secretman config edit <key>   change one")
			ui.Note("  secretman config rm <key>     remove one")
			ui.Blank()
			return nil
		}
	}

	if err := requireTTY("init"); err != nil {
		return err
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if d := filepath.Dir(path); d != "" && d != "." {
		dir = d
	}
	cfg, err := wizardConfig(ctx, dir)
	if err != nil {
		return err
	}

	if flagInitPrint {
		out, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	}

	if err := config.Save(cfg, path); err != nil {
		return err
	}
	ui.Blank()
	ui.OK("wrote %s", path)
	ui.Note("commit it — it names secrets, it holds none")

	// Everything from here writes to a store, and every failure past this point
	// leaves a config that is still correct, so none of it is fatal to the file.
	flagConfig = path
	return provision(ctx)
}

// initTarget decides which file init is about, and whether it already exists.
func initTarget() (path string, existing bool, err error) {
	if flagConfig != "" {
		if st, err := os.Stat(flagConfig); err == nil && !st.IsDir() {
			return flagConfig, true, nil
		}
		return flagConfig, false, nil
	}

	found, err := config.Find("")
	if err != nil {
		return "", false, err
	}
	if found != "" {
		return found, true, nil
	}

	path, err = config.DefaultPath("")
	return path, false, err
}

func confirmOverwrite(path string) (bool, error) {
	if flagInitForce {
		ui.Warn("--force: replacing %s", path)
		return true, nil
	}
	if err := requireTTY("init"); err != nil {
		return false, err
	}

	// The existing config is loaded only to say what is about to be detached.
	// A config too broken to load is still worth overwriting, so a failure here
	// is reported and moved past.
	summary := "the existing configuration"
	if cfg, err := config.LoadRaw(path, ""); err == nil {
		summary = fmt.Sprintf("%d secret(s) — %v", len(cfg.Secrets), cfg.SecretKeys())
	}

	ui.Blank()
	ui.Warn("A config already exists. Replacing it DETACHES what it describes:")
	ui.Warn("  %s", summary)
	ui.Warn("  Nothing is deleted from GitHub or Secret Manager, but any secret")
	ui.Warn("  not in the new config stops being rotated, verified or listed,")
	ui.Warn("  and this tool will never mention it again.")
	ui.Blank()

	overwrite := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Replace this config from scratch?").
			Description("config add / edit / rm change one secret instead").
			Affirmative("Replace it").
			Negative("Leave it alone").
			Value(&overwrite),
	)).WithTheme(theme()).Run()
	return overwrite, err
}

// provision creates the GitHub Environments the config declares.
func provision(ctx context.Context) error {
	s, err := loadSession()
	if err != nil {
		return err
	}

	ui.Title("%s › init", s.cfg.Project)
	if err := s.preflight(ctx); err != nil {
		return err
	}

	did := false
	for _, st := range s.stores {
		p, ok := st.(store.Provisioner)
		if !ok {
			continue
		}
		ui.Step("%s", st.Label())
		if err := p.EnsureEnvironments(ctx, s.cfg.Environments, flagDryRun, ui.Log); err != nil {
			return err
		}
		did = true
	}
	if !did {
		ui.Blank()
		ui.Note("no store needs provisioning")
	}

	ui.Step("Done")
	ui.Note("run `secretman rotate <environment>` to populate the secrets")
	ui.Blank()
	return nil
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the config and every store's credentials, and stop",
		Long: `Runs the same preflight a rotation runs, and nothing else.

Worth its own command because every preflight failure here — a wrong project id,
an expired login, an API that was never enabled — is one that would otherwise be
discovered halfway through a rotation, with one store already written and the
other not.

Nothing is prompted for and nothing is written, so it is safe in CI and safe to
run while someone else is mid-rotation.`,
		Example: `  secretman doctor
  secretman doctor --store gcp
  secretman doctor -c ../other-project/.secretman.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSession()
			if err != nil {
				return err
			}
			ui.Title("%s › doctor", s.cfg.Project)
			if err := s.preflight(cmd.Context()); err != nil {
				return err
			}

			ui.Step("Configuration")
			ui.Note("environments: %v", s.cfg.EnvNames())
			ui.Note("secrets:      %v", s.cfg.SecretKeys())
			for _, st := range s.stores {
				verb := "write-only"
				if st.Readable() {
					verb = "readable — verify and cross-check work here"
				}
				ui.Note("%-24s %s", st.Label(), verb)
			}

			ui.Blank()
			ui.OK("ready to rotate")
			ui.Blank()
			return nil
		},
	}
}
