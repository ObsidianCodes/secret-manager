package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/secretval"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Create and remove secrets, and show the config",
		Long: `Changes what exists in the stores, and shows what this tool is pointed at.

rotate only ever walks what already exists, so this is where a secret that does
not exist yet gets created. There is no local list to keep in step: add writes
straight to the stores, and the next rotate sees it because it asks them.`,
		Example: `  secretman config add       # create a secret in one or more stores
  secretman config rm        # delete one, permanently, after confirming
  secretman config show      # the three fields this tool is pointed at
  secretman config path`,
	}
	c.AddCommand(newConfigAddCmd(), newConfigRemoveCmd(),
		newConfigShowCmd(), newConfigPathCmd())
	return c
}

// --------------------------------------------------------------- add

func newConfigAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Create a secret that does not exist yet",
		Long: `Asks for a name, where it belongs, and a value, then writes it.

A secret has to exist before it can be enumerated, and enumeration is the only
list this tool has. So creation is its own command, and this is it.

The name is asked once per store, because the stores spell things differently:
GitHub wants WORKOS_API_KEY, Secret Manager wants a prefixed lowercase name.
The suggested Secret Manager name follows from the prefix, but nothing enforces
it — an existing naming scheme is adopted rather than fought.`,
		Example: `  secretman config add`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigAdd(cmd.Context())
		},
	}
}

func runConfigAdd(ctx context.Context) error {
	if err := requireTTY("config add"); err != nil {
		return err
	}
	s, err := loadSession()
	if err != nil {
		return err
	}
	ui.Title("secretman › config add")
	if err := s.preflight(ctx); err != nil {
		return err
	}

	// What GitHub environments exist is a question only GitHub can answer, and
	// the answer decides what this command can offer.
	var envs []string
	if s.gh != nil {
		envs, _ = s.gh.Environments(ctx)
	}

	name := ""
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Secret name").
			Description("as an environment variable, e.g. WORKOS_API_KEY").
			Value(&name).
			Validate(notBlank("secret name")),
	)).WithTheme(theme()).Run(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)

	targets, err := s.chooseTargets(name, envs)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		ui.Blank()
		ui.Note("Nothing selected.")
		return nil
	}

	value, generated, err := askValue(name)
	if err != nil {
		return err
	}
	if value == "" {
		ui.Blank()
		ui.Note("No value given; nothing created.")
		return nil
	}

	values := make([]pending, 0, len(targets))
	for _, e := range targets {
		values = append(values, pending{entry: e, value: value, generated: generated,
			reused: len(targets) > 1})
	}

	if err := s.confirm(values); err != nil {
		return err
	}
	if flagDryRun {
		ui.Blank()
		ui.OK("dry run complete; nothing written")
		return nil
	}
	return s.write(ctx, values)
}

// chooseTargets asks where a new secret should live, one store at a time.
func (s *session) chooseTargets(name string, envs []string) ([]store.Entry, error) {
	var out []store.Entry

	if s.gh != nil {
		opts := []huh.Option[string]{
			huh.NewOption("repository-wide (every workflow)", ""),
		}
		for _, env := range envs {
			opts = append(opts, huh.NewOption("environment: "+env, env))
		}

		var chosen []string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("GitHub Actions — where does " + name + " belong?").
				Description("none is a valid answer · `secretman env add` creates an environment").
				Options(opts...).
				Value(&chosen),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}
		for _, env := range chosen {
			out = append(out, store.Entry{Store: "github", Name: name, Env: env})
		}
	}

	if s.gcp != nil {
		gcpName := s.gcp.Prefix() + strings.ToLower(strings.ReplaceAll(name, "_", "-"))
		include := true
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title("Also create this in Google Secret Manager?").
				Value(&include),
			huh.NewInput().
				Title("Secret Manager name").
				Description("Secret Manager has no environments, so anything distinguishing "+
					"them lives in the name").
				Value(&gcpName).
				Validate(notBlank("Secret Manager name")),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}
		if include {
			out = append(out, store.Entry{Store: "gcp", Name: strings.TrimSpace(gcpName)})
		}
	}

	return out, nil
}

// askValue prompts for the value of a new secret.
func askValue(name string) (value string, generated bool, err error) {
	mode := "type"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Value for "+name).
			Options(
				huh.NewOption("1. Type or paste a value", "type"),
				huh.NewOption("2. Generate a random value", "generate"),
			).
			Value(&mode),
	)).WithTheme(theme()).Run(); err != nil {
		return "", false, err
	}

	if mode == "generate" {
		v, err := secretval.Generate(0)
		if err != nil {
			return "", false, err
		}
		ui.OK("generated, %d characters, %s", len(v),
			ui.Fingerprint(secretval.Fingerprint(v)))
		return v, true, nil
	}

	var raw string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(name).
			EchoMode(huh.EchoModePassword).
			Value(&raw).
			Validate(func(in string) error {
				if strings.TrimSpace(in) == "" {
					return nil
				}
				_, _, err := secretval.Sanitize(name, in)
				return err
			}),
	)).WithTheme(theme()).Run(); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(raw) == "" {
		return "", false, nil
	}

	clean, notes, err := secretval.Sanitize(name, raw)
	if err != nil {
		return "", false, err
	}
	for _, n := range notes {
		ui.Warn("%s", n)
	}
	ui.OK("%d characters, %s", len(clean), ui.Fingerprint(secretval.Fingerprint(clean)))
	return clean, false, nil
}

// --------------------------------------------------------------- rm

func newConfigRemoveCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:     "rm",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a secret from a store, permanently",
		Long: `Deletes the secret itself, not a reference to it.

There is no local list to remove something from, so this reaches into the store
and destroys it. In Secret Manager that takes every version with it, and no
rollback exists afterwards. Anything still reading that secret starts failing at
its next cold start.

If the aim is to stop rotating something rather than to destroy it, there is
nothing to do: rotate walks what exists, and Enter skips.`,
		Example: `  secretman config rm
  secretman config rm --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigRemove(cmd.Context(), force)
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "do not ask")
	return c
}

func runConfigRemove(ctx context.Context, force bool) error {
	if err := requireTTY("config rm"); err != nil {
		return err
	}
	s, entries, err := gatherQuiet(ctx, "config rm")
	if err != nil {
		return err
	}

	chosen, err := pickEntries(entries, "Delete which secrets?",
		"this destroys them in the store — it does not merely unmanage them")
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		ui.Blank()
		ui.Note("Nothing selected.")
		return nil
	}

	ui.Blank()
	for _, e := range chosen {
		ui.Warn("delete %s", e.Label())
	}
	ui.Warn("")
	ui.Warn("  This is permanent. Every version goes with it, and anything still")
	ui.Warn("  reading it fails at its next cold start.")
	ui.Blank()

	if flagDryRun {
		ui.OK("dry run complete; nothing deleted")
		return nil
	}

	if !force {
		ok := false
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Delete %d secret(s)?", len(chosen))).
				Affirmative("Delete them").
				Negative("Keep them").
				Value(&ok),
		)).WithTheme(theme()).Run(); err != nil {
			return err
		}
		if !ok {
			ui.Note("Nothing deleted.")
			return nil
		}
	}

	ui.Step("Deleting")
	failed := 0
	for _, e := range chosen {
		if err := s.delete(ctx, e); err != nil {
			ui.Warn("%s: FAILED — %v", e.Label(), err)
			failed++
			continue
		}
		ui.OK("%s — deleted", e.Label())
	}
	ui.Blank()
	if failed > 0 {
		return fmt.Errorf("%d delete(s) failed", failed)
	}
	return nil
}

// --------------------------------------------------------------- show, path

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the config",
		Long: `Prints the file as written. It is three fields; there is nothing to resolve.

To see what actually exists in the stores, use status.`,
		Example: `  secretman config show`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath()
			if err != nil {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Print(string(b))
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path of the config in effect",
		Long: `Prints where the config was found, and nothing else.

Which file is in effect depends on the working directory, so it is worth being
able to ask.`,
		Example: `  secretman config path`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath()
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

func configPath() (string, error) {
	if flagConfig != "" {
		return flagConfig, nil
	}
	return config.Discover("")
}

// delete removes a secret from its store. There is no soft form of this: the
// stores have no notion of "unmanaged", so the only thing to delete is the
// secret itself.
func (s *session) delete(ctx context.Context, e store.Entry) error {
	switch e.Store {
	case "github":
		args := []string{"secret", "delete", e.Name, "--repo", s.gh.Repo()}
		if e.Env != "" {
			args = append(args, "--env", e.Env)
		}
		_, err := shell.Run(ctx, "gh", args...)
		return err
	case "gcp":
		_, err := shell.Run(ctx, "gcloud", "secrets", "delete", e.Name,
			"--project="+s.gcp.Project(), "--quiet")
		return err
	}
	return fmt.Errorf("unknown store %q", e.Store)
}
