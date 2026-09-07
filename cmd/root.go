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

It reads a .secretman.yaml describing the project's environments and secrets, so
one binary serves every project rather than one script per repository. That file
holds names; it never holds a value, and is meant to be committed.

Getting started:

  secretman init                  create the config, then the environments
  secretman doctor                check the config and every store's login
  secretman rotate staging        prompt, validate, write, verify
  secretman status                what exists where

Changing what is managed:

  secretman config add            add a secret
  secretman config edit <key>     change one
  secretman config rm <key>       stop managing one
  secretman config schema         every config field, annotated

What a rotation protects against, in order of how expensive each is to diagnose
later:

  * a trailing newline on a pasted value, which is invisible in every UI that
    displays a secret, and which no store strips for you
  * a value another environment already holds, pasted into this one, which is
    accepted everywhere and only fails later, in production
  * a value mangled by a terminal that wrapped the paste
  * a secret written to one store but not the other, leaving them disagreeing
    about which credential is current

It does not check what a valid value looks like. A rule of that shape needs a
provider's current key format written down, goes stale without saying so, and
then refuses a correct credential mid-rotation. Every prompt instead prints the
store, environment and name it is about to overwrite.

Values are typed at a hidden prompt, passed to gh and gcloud on stdin, and never
appear in argv, shell history, or this tool's output. Only digests are printed.

Requires gh and gcloud on PATH, both already logged in. secretman reuses their
credentials, and has none of its own.`,
		Example: `  # First run in a repository
  secretman init

  # Routine rotation
  secretman rotate staging

  # One secret, right now, no picker
  secretman rotate production --only workos-api-key

  # Rehearse without writing anything
  secretman rotate production --dry-run

  # Which secrets exist, where
  secretman status
  secretman verify`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&flagConfig, "config", "c", "", "path to .secretman.yaml (default: found by walking up from the working directory)")
	pf.StringVar(&flagGCPProject, "gcp-project", "", "override the GCP project id from the config")
	pf.StringVar(&flagRepo, "repo", "", "override the GitHub repository (owner/name)")
	pf.StringSliceVar(&flagStores, "store", nil, "limit to these stores: github, gcp")
	pf.BoolVar(&flagDryRun, "dry-run", false, "prompt and validate, but write nothing")
	pf.BoolVarP(&flagYes, "yes", "y", false, "skip the confirmation prompt")
	pf.BoolVar(&flagNoCheck, "no-cross-check", false, "skip the checks that refuse another environment's value")

	root.AddCommand(
		newInitCmd(),
		newConfigCmd(),
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
