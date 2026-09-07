package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which secrets exist, in which environment, in which store",
		Long: `Prints a matrix of secret × environment for every store.

This is what the bash script could not do at all, and it is the question most
often asked in an incident: is production actually configured, or has it been
running on a value nobody has set since the last person left?

Presence only. No value is read, and nothing here says whether two environments
hold the same value — for that, use verify.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := loadSession()
			if err != nil {
				return err
			}

			ui.Title("%s › status", s.cfg.Project)
			if err := s.preflight(ctx); err != nil {
				return err
			}

			for _, st := range s.stores {
				// Presence per environment, fetched once per environment
				// rather than once per cell.
				present := map[string]map[string]bool{}
				for _, e := range s.cfg.Environments {
					names, err := st.List(ctx, e)
					if err != nil {
						return fmt.Errorf("%s: listing %s: %w", st.Label(), e.Name, err)
					}
					present[e.Name] = names
				}

				headers := append([]string{"SECRET"}, s.cfg.EnvNames()...)
				var rows [][]string
				for _, sec := range s.cfg.Secrets {
					if !sec.UsesStore(st.ID()) {
						continue
					}
					row := []string{sec.Name}
					for _, e := range s.cfg.Environments {
						if present[e.Name][lastSegment(st.Target(sec, e))] {
							row = append(row, ui.Present())
						} else {
							row = append(row, ui.Absent())
						}
					}
					rows = append(rows, row)
				}

				ui.Step("%s", st.Label())
				fmt.Println(ui.Indent(ui.Table(headers, rows), 2))
			}

			ui.Blank()
			ui.Note("%s set   %s missing", ui.Present(), ui.Absent())
			ui.Blank()
			return nil
		},
	}
}
