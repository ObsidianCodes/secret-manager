// Package cmd wires the CLI.
package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

// Version is set at build time with -ldflags "-X .../cmd.Version=...".
var Version = "dev"

var (
	flagConfig string
	flagDryRun bool
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
		Long: `secretman rotates a project's credentials in the stores that hold them.

It keeps no list of your secrets. The config file holds three fields — which
repository, which GCP project, which name prefix — and everything else is read
from the stores themselves on every run. Nothing can be out of date, because
nothing is remembered.

A rotation walks what actually exists. Each stop prints the store, the name as
that store spells it, and the environment when there is one, then asks for a
value. Enter skips.

  secretman init                  write the config: repo, project, prefix
  secretman rotate                walk everything, prompt for each
  secretman hotswap               pick a few from a list, walk only those
  secretman config add            create a secret that does not exist yet
  secretman delete                destroy secrets, permanently, after asking twice
  secretman var                   the non-secret half: GitHub Actions variables
  secretman status                what exists where, and what is missing
  secretman verify                read back: shared values, damaged values
  secretman doctor                check credentials, change nothing

Values are typed at a hidden prompt, passed to gh and gcloud on stdin, and never
appear in argv, shell history, or this tool's output. Only digests are printed.

Requires gh and gcloud on PATH, both already logged in. secretman reuses their
credentials, and has none of its own.`,
		Example: `  secretman init                # first run in a repository
  secretman rotate              # walk every secret in every store
  secretman hotswap             # pick the two that leaked, rotate those
  secretman status`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&flagConfig, "config", "c", "",
		"path to .secretman.yaml (default: found by walking up from the working directory)")
	pf.BoolVar(&flagDryRun, "dry-run", false, "prompt and check, but write nothing")

	root.AddCommand(
		newInitCmd(),
		newConfigCmd(),
		newRotateCmd(),
		newHotswapCmd(),
		newDeleteCmd("delete"),
		newStatusCmd(),
		newVerifyCmd(),
		newDoctorCmd(),
		newEnvCmd(),
		newVarCmd(),
	)
	return root
}

// session is the shared setup every command needs.
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

	s := &session{cfg: cfg}
	if cfg.GitHub != nil {
		s.gh = store.NewGitHub(cfg)
		s.stores = append(s.stores, s.gh)
	}
	if cfg.GCP != nil {
		s.gcp = store.NewGCP(cfg, "")
		s.stores = append(s.stores, s.gcp)
	}
	return s, nil
}

// preflight checks every store before anything is prompted for.
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
			ui.OK("%s — project %s, prefix %q", v.Label(), v.Project(), v.Prefix())
		default:
			ui.OK("%s", st.Label())
		}
	}
	return nil
}

// enumerate asks every store what it currently holds.
//
// This is the whole state of the world, fetched once per command. Nothing is
// cached between runs, which is what makes a stale list impossible.
func (s *session) enumerate(ctx context.Context) ([]store.Entry, error) {
	var all []store.Entry
	for _, st := range s.stores {
		got, err := st.Enumerate(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", st.Label(), err)
		}
		all = append(all, got...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Store != all[j].Store {
			return all[i].Store < all[j].Store
		}
		if all[i].Env != all[j].Env {
			return all[i].Env < all[j].Env
		}
		return all[i].Name < all[j].Name
	})
	return all, nil
}

// storeFor finds the store an entry belongs to.
func (s *session) storeFor(e store.Entry) store.Store {
	for _, st := range s.stores {
		if st.ID() == e.Store {
			return st
		}
	}
	return nil
}
