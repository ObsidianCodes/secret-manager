package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

var (
	flagInitForce bool
	flagInitPrint bool
)

func newInitCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "init",
		Short: "Write .secretman.yaml: repository, GCP project, name prefix",
		Long: `Asks three things and writes them down.

  github.repo    which repository holds the Actions secrets
  gcp.project    which GCP project holds the Secret Manager secrets, picked
                 from the list gcloud can see — never typed
  gcp.prefix     which name prefix within it belongs to this project

That is the entire config. No secret is named in it, so it cannot be wrong
about one — what exists is read from the stores on every run.

If a config already exists, init stops and asks before replacing it, because
replacing it is not an edit. Changing the repository or the prefix points
secretman at a different set of secrets: the ones it points at now keep
existing, keep working, and stop being visible to this tool entirely.

init creates nothing in any store. Environments are created by ` + "`secretman env add`" + `,
and secrets by ` + "`secretman config add`" + `.`,
		Example: `  secretman init
  secretman init --print      # show what it would write, write nothing
  secretman init --force      # replace an existing config without asking`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.Context())
		},
	}
	c.Flags().BoolVarP(&flagInitForce, "force", "f", false,
		"replace an existing config without asking")
	c.Flags().BoolVar(&flagInitPrint, "print", false,
		"print the config to stdout instead of writing it")
	return c
}

func runInit(ctx context.Context) error {
	path, existing, err := initTarget()
	if err != nil {
		return err
	}

	ui.Title("secretman › init")

	if !existing {
		noteInheritedConfig(path)
	} else {
		ui.Note("config %s", path)
		ok, err := confirmReplace(path)
		if err != nil {
			return err
		}
		if !ok {
			ui.Blank()
			ui.Note("Nothing written.")
			ui.Blank()
			return nil
		}
	}

	if err := requireTTY("init"); err != nil {
		return err
	}

	cfg, err := wizardConfig(ctx)
	if err != nil {
		return err
	}

	if !initWillWrite(flagInitPrint, flagDryRun) {
		out, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		if flagInitPrint {
			fmt.Print(string(out))
			return nil
		}
		ui.Blank()
		ui.Step("Would write %s", path)
		fmt.Println(ui.Indent(strings.TrimRight(string(out), "\n"), 2))
		ui.Blank()
		ui.OK("dry run complete; nothing written")
		ui.Blank()
		return nil
	}

	if err := config.Save(cfg, path); err != nil {
		return err
	}

	ui.Blank()
	ui.OK("wrote %s", path)
	ui.Note("commit it — it names no secret and holds no value")
	ui.Blank()
	ui.Note("next:")
	ui.Note("  secretman doctor       check both stores are reachable")
	ui.Note("  secretman rotate       walk whatever already exists")
	ui.Note("  secretman config add   create a secret that does not")
	ui.Blank()
	return nil
}

// initTarget decides which file init is about, and whether it already exists.
//
// Deliberately does NOT walk up the way every other command does. A config in a
// parent directory belongs to a different project, and treating it as this
// project's makes init refuse to initialise anything nested inside another
// repository — which is the one thing init is for. Only the file at the target
// path counts.
func initTarget() (path string, existing bool, err error) {
	if flagConfig != "" {
		path = flagConfig
	} else if path, err = config.DefaultPath(""); err != nil {
		return "", false, err
	}
	st, err := os.Stat(path)
	return path, err == nil && !st.IsDir(), nil
}

// noteInheritedConfig mentions a config in a parent directory.
//
// It is not this project's and init will not touch it, but it is what every
// other command run from here would have used until this moment, so silence
// about it would be its own surprise.
func noteInheritedConfig(path string) {
	found, err := config.Find("")
	if err != nil || found == "" || found == path {
		return
	}
	if abs, err := filepath.Abs(path); err == nil && abs == found {
		return
	}
	ui.Note("a config also exists at %s, in a parent directory", found)
	ui.Note("it belongs to whatever lives there; this writes a new one here")
}

func confirmReplace(path string) (bool, error) {
	// What the existing config points at is worth printing, because that is
	// precisely what is about to stop being visible.
	if cfg, err := config.Load(path, ""); err == nil {
		ui.Blank()
		ui.Warn("This project already points somewhere, and secrets are set there:")
		if cfg.GitHub != nil {
			ui.Warn("  github  %s", orAuto(cfg.GitHub.Repo))
		}
		if cfg.GCP != nil {
			ui.Warn("  gcp     %s, prefix %q", cfg.GCP.Project, cfg.GCP.Prefix)
		}
		ui.Warn("")
		ui.Warn("  Replacing this points secretman at a different set of secrets.")
		ui.Warn("  Nothing is deleted: the secrets it points at now keep existing and")
		ui.Warn("  keep working. They simply stop being listed, rotated or verified,")
		ui.Warn("  and nothing here will mention them again.")
		ui.Blank()
	}

	if flagInitForce {
		ui.Warn("--force: replacing %s", path)
		return true, nil
	}
	if err := requireTTY("init"); err != nil {
		return false, err
	}

	replace := false
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Replace this config?").
			Affirmative("Replace it").
			Negative("Leave it alone").
			Value(&replace),
	)).WithTheme(theme()).Run()
	return replace, err
}

// wizardConfig asks for the three fields.
func wizardConfig(ctx context.Context) (*config.Config, error) {
	stores := []string{"github", "gcp"}
	repo := detectRepo(ctx)
	gcpProject := detectGCPProject(ctx)
	prefix := defaultPrefix(repo)

	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Which stores hold this project's secrets?").
			Description("space to toggle · enter to continue").
			Options(
				huh.NewOption("GitHub Actions — repository and environment secrets, via gh", "github").Selected(true),
				huh.NewOption("Google Secret Manager — versioned secrets, via gcloud", "gcp").Selected(true),
			).
			Value(&stores).
			Validate(func(v []string) error {
				if len(v) == 0 {
					return fmt.Errorf("pick at least one store")
				}
				return nil
			}),
	)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}

	cfg := &config.Config{}

	if has(stores, "github") {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("GitHub repository").
				Description("owner/name · blank lets gh resolve it from the working directory").
				Placeholder("owner/name").
				Value(&repo).
				Validate(optional(looksLikeRepo)),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}
		cfg.GitHub = &config.GitHub{Repo: strings.TrimSpace(repo)}
	}

	if has(stores, "gcp") {
		projects, err := listGCPProjects(ctx)
		if err != nil {
			return nil, err
		}
		if len(projects) == 0 {
			return nil, errNoGCPProjects
		}
		// The current gcloud project, when it is one of these, is only a
		// starting position in the list — never an answer nobody looked at.
		if !hasProject(projects, gcpProject) {
			gcpProject = projects[0].ID
		}

		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("GCP project").
				Description("type to filter · enter to pick").
				Options(projectOptions(projects)...).
				Height(12).
				Filtering(true).
				Value(&gcpProject).
				Validate(notBlank("GCP project")),

			huh.NewInput().
				Title("Secret name prefix").
				Description("everything carrying it is this project's; everything else in the "+
					"GCP project is left alone").
				Value(&prefix),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}
		cfg.GCP = &config.GCP{
			Project: strings.TrimSpace(gcpProject),
			Prefix:  strings.TrimSpace(prefix),
		}
	}

	return cfg, cfg.Validate()
}

// initWillWrite says whether init may touch the filesystem.
//
// --print puts the config on stdout to be piped somewhere; --dry-run is a
// rehearsal. Both stop before the write. A config written during a rehearsal is
// a file the operator did not ask for, left in a directory they were only
// looking at, and --dry-run promises no file is created anywhere.
func initWillWrite(print, dryRun bool) bool { return !print && !dryRun }

func detectRepo(ctx context.Context) string {
	res, err := shell.Run(ctx, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

func detectGCPProject(ctx context.Context) string {
	res, err := shell.Run(ctx, "gcloud", "config", "get-value", "project")
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(res.Stdout)
	if v == "(unset)" {
		return ""
	}
	return v
}

// gcpProject is one project the active gcloud account can see.
type gcpProject struct {
	ID   string `json:"projectId"`
	Name string `json:"name"`
}

// errNoGCPProjects is the dead end: there is nothing to pick, and creating a
// project is not this tool's job. It creates no project, no environment and no
// secret — it only points at ones that exist.
var errNoGCPProjects = fmt.Errorf("the active gcloud account can see no GCP projects\n" +
	"  secretman does not create projects. Create one first:\n" +
	"    gcloud projects create <id>\n" +
	"  or, if this is the wrong account, switch it:\n" +
	"    gcloud auth login\n" +
	"  then run `secretman init` again")

// listGCPProjects asks gcloud what the active account can see.
//
// The project id is never typed here. It is not the project NAME, the two are
// usually different, and a typo produces a config that points at nothing —
// discovered later, by a rotation, with the other store already written.
func listGCPProjects(ctx context.Context) ([]gcpProject, error) {
	if err := shell.Require("gcloud"); err != nil {
		return nil, fmt.Errorf("%w\n  init needs it to list the GCP projects you can see", err)
	}

	var (
		out []gcpProject
		err error
	)
	ui.Blank()
	_ = spinner.New().
		Title(" asking gcloud which projects you can see…").
		Action(func() { out, err = fetchGCPProjects(ctx) }).
		Run()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fetchGCPProjects(ctx context.Context) ([]gcpProject, error) {
	res, err := shell.Run(ctx, "gcloud", "projects", "list",
		"--sort-by=projectId", "--format=json(projectId,name)")
	if err != nil {
		return nil, fmt.Errorf("listing GCP projects: %w\n"+
			"  if the account is not logged in, run: gcloud auth login", err)
	}
	var rows []gcpProject
	if err := json.Unmarshal([]byte(res.Stdout), &rows); err != nil {
		return nil, fmt.Errorf("parsing gcloud projects list: %w", err)
	}
	return rows, nil
}

func projectOptions(projects []gcpProject) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(projects))
	for _, p := range projects {
		label := p.ID
		// The display name is shown because it is what the operator recognises,
		// and the id is what gets written, so both have to be visible at once.
		if p.Name != "" && p.Name != p.ID {
			label += "  (" + p.Name + ")"
		}
		opts = append(opts, huh.NewOption(label, p.ID))
	}
	return opts
}

func hasProject(projects []gcpProject, id string) bool {
	for _, p := range projects {
		if p.ID == id {
			return true
		}
	}
	return false
}

// defaultPrefix suggests <repo-name>- so the GCP project can hold more than one
// project's secrets without them colliding.
func defaultPrefix(repo string) string {
	name := repo
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		if wd, err := os.Getwd(); err == nil {
			name = filepath.Base(wd)
		}
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	return strings.ToLower(name) + "-"
}

func orAuto(s string) string {
	if s == "" {
		return "(resolved by gh from the working directory)"
	}
	return s
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the config and every store's credentials, and stop",
		Long: `Runs the same preflight a rotation runs, then reports what it can see.

Worth its own command because every failure here — a wrong project id, an
expired login, an API that was never enabled — is one that would otherwise be
discovered halfway through a rotation, with one store already written and the
other not.

Nothing is prompted for and nothing is written, so it is safe in CI and safe to
run while someone else is mid-rotation.`,
		Example: `  secretman doctor
  secretman doctor -c ../other-project/.secretman.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := loadSession()
			if err != nil {
				return err
			}
			ui.Title("secretman › doctor")
			if err := s.preflight(ctx); err != nil {
				return err
			}

			entries, err := s.enumerate(ctx)
			if err != nil {
				return err
			}

			ui.Step("Visible")
			for _, st := range s.stores {
				n := 0
				for _, e := range entries {
					if e.Store == st.ID() {
						n++
					}
				}
				verb := "write-only"
				if st.Readable() {
					verb = "readable — verify works here"
				}
				ui.Note("%-24s %2d secret(s), %s", st.Label(), n, verb)
			}
			if s.gh != nil {
				envs, _ := s.gh.Environments(ctx)
				ui.Note("%-24s %v", "GitHub environments", envs)
			}

			ui.Blank()
			if len(entries) == 0 {
				ui.Warn("no secrets exist yet; `secretman config add` creates one")
			} else {
				ui.OK("ready to rotate")
			}
			ui.Blank()
			return nil
		},
	}
}
