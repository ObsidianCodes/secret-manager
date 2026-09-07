package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create everything a rotation needs, without writing any secret",
		Long: `Creates the GitHub Environments declared in the config.

An environment-scoped secret cannot be written until its environment exists, and
the create is idempotent, so rotate does this too. init exists to get a fresh
repository into a rotatable state before anyone has a credential in hand.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the config and every store's credentials, and stop",
		Long: `Runs the same preflight a rotation runs, and nothing else.

Worth its own command because every preflight failure here — a wrong project id,
an expired login, an API that was never enabled — is one that would otherwise be
discovered halfway through a rotation, with one store already written and the
other not.`,
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
