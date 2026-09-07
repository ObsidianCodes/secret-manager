package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newEnvCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "env",
		Short: "List and create GitHub Environments",
		Long: `GitHub Environments, which are what scopes a secret to staging or production.

An environment-scoped secret cannot be written until its environment exists, so
a new environment is created here before anything is put in it. Secret Manager
has no equivalent: there, an environment is part of the name.`,
		Example: `  secretman env list
  secretman env add staging`,
	}
	c.AddCommand(newEnvListCmd(), newEnvAddCmd())
	return c
}

func newEnvListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the repository's GitHub Environments",
		Example: `  secretman env list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSession()
			if err != nil {
				return err
			}
			if s.gh == nil {
				return fmt.Errorf("no github block in the config; environments are a GitHub concept")
			}
			if err := s.gh.Preflight(cmd.Context()); err != nil {
				return err
			}
			envs, err := s.gh.Environments(cmd.Context())
			if err != nil {
				return err
			}
			if len(envs) == 0 {
				ui.Note("no environments; every secret here is repository-wide")
				return nil
			}
			for _, e := range envs {
				fmt.Println(e)
			}
			return nil
		},
	}
}

func newEnvAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name>...",
		Short: "Create GitHub Environments",
		Long: `Creates each named environment if it does not already exist.

The underlying PUT is idempotent, so naming one that exists is not an error and
costs one API call.`,
		Example: `  secretman env add staging
  secretman env add staging production development`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := loadSession()
			if err != nil {
				return err
			}
			if s.gh == nil {
				return fmt.Errorf("no github block in the config; environments are a GitHub concept")
			}
			ui.Title("secretman › env add")
			if err := s.gh.Preflight(cmd.Context()); err != nil {
				return err
			}

			ui.Step("GitHub Actions")
			for _, name := range args {
				if flagDryRun {
					ui.Note("%s (dry run — not creating it)", name)
					continue
				}
				created, err := s.gh.EnsureEnvironment(cmd.Context(), name)
				if err != nil {
					return err
				}
				if created {
					ui.OK("created environment %s", name)
				} else {
					ui.OK("environment %s already exists", name)
				}
			}
			ui.Blank()
			return nil
		},
	}
}
