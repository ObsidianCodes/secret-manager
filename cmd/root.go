// Package cmd wires the CLI.
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

// Version is set at build time with -ldflags "-X .../cmd.Version=...".
var Version = "dev"

var (
	flagConfig     string
	flagGCPProject string
	flagRepo       string
	flagStores     []string
	flagDryRun     bool
	flagYes        bool
	flagNoCheck    bool
)

// Execute runs the CLI.
func Execute() {
	if err := newRoot().Execute(); err != nil {
		ui.Err("%v", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "secretman",
		Short: "Rotate project credentials across GitHub Actions and Google Secret Manager",
		Long: `secretman rotates a project's credentials in every store that holds them,
one environment at a time.

It reads a secrets.yaml describing the project's environments and secrets, so
one binary serves every project rather than one script per repository.

What it protects against, in order of how expensive each is to diagnose later:

  * a trailing newline on a pasted value, which is invisible in every UI that
    displays a secret, and which no store strips for you
  * a value pasted into the wrong environment's prompt, which is accepted
    everywhere and only fails in production
  * two adjacent dashboard fields transposed, which fails at the first login
  * a secret written to one store but not the other, leaving them disagreeing
    about which credential is current

Values are typed at a hidden prompt, passed to gh and gcloud on stdin, and never
appear in argv, shell history, or this tool's output.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&flagConfig, "config", "c", "", "path to secrets.yaml (default: found by walking up from the working directory)")
	pf.StringVar(&flagGCPProject, "gcp-project", "", "override the GCP project id from the config")
	pf.StringVar(&flagRepo, "repo", "", "override the GitHub repository (owner/name)")
	pf.StringSliceVar(&flagStores, "store", nil, "limit to these stores: github, gcp")
	pf.BoolVar(&flagDryRun, "dry-run", false, "prompt and validate, but write nothing")
	pf.BoolVarP(&flagYes, "yes", "y", false, "skip the confirmation prompt")
	pf.BoolVar(&flagNoCheck, "no-cross-check", false, "skip the checks that refuse another environment's value")

	root.AddCommand(
		newInitCmd(),
		newRotateCmd(),
		newStatusCmd(),
		newVerifyCmd(),
		newDoctorCmd(),
	)
	return root
}

// session is the shared setup every command needs: a loaded config and the
// stores it selects.
type session struct {
	cfg    *config.Config
	stores []store.Store
	gh     *store.GitHub
	gcp    *store.GCP
}

func loadSession() (*session, error) {
	cfg, err := config.Load(flagConfig, "")
	if err != nil {
		return nil, err
	}
	if flagRepo != "" {
		if cfg.GitHub == nil {
			cfg.GitHub = &config.GitHub{}
		}
		cfg.GitHub.Repo = flagRepo
	}

	s := &session{cfg: cfg}
	if cfg.GitHub != nil && wantStore("github") {
		s.gh = store.NewGitHub(cfg)
		s.stores = append(s.stores, s.gh)
	}
	if cfg.GCP != nil && wantStore("gcp") {
		s.gcp = store.NewGCP(cfg, flagGCPProject)
		s.stores = append(s.stores, s.gcp)
	}
	if len(s.stores) == 0 {
		return nil, fmt.Errorf("no stores selected; check --store and the config")
	}
	return s, nil
}

func wantStore(id string) bool {
	if len(flagStores) == 0 {
		return true
	}
	for _, want := range flagStores {
		if want == id {
			return true
		}
	}
	return false
}

// preflight checks every selected store before anything is prompted for.
func (s *session) preflight(ctx context.Context) error {
	ui.Step("Preflight")
	ui.Note("config %s", s.cfg.Path)
	for _, st := range s.stores {
		if err := st.Preflight(ctx); err != nil {
			return fmt.Errorf("%s: %w", st.Label(), err)
		}
		switch v := st.(type) {
		case *store.GitHub:
			ui.OK("%s — repository %s", v.Label(), v.Repo())
		case *store.GCP:
			ui.OK("%s — project %s", v.Label(), v.Project())
		default:
			ui.OK("%s", st.Label())
		}
	}
	return nil
}

// storesFor filters the session's stores to those a secret uses.
func (s *session) storesFor(sec config.Secret) []store.Store {
	var out []store.Store
	for _, st := range s.stores {
		if sec.UsesStore(st.ID()) {
			out = append(out, st)
		}
	}
	return out
}

// resolveEnv turns a name into a configured environment, with a useful error.
func (s *session) resolveEnv(name string) (config.Environment, error) {
	e, ok := s.cfg.Env(name)
	if !ok {
		return config.Environment{}, fmt.Errorf("unknown environment %q; configured: %v",
			name, s.cfg.EnvNames())
	}
	return e, nil
}
