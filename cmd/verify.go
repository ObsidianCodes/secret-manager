package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/secretval"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check stored values for shared credentials and stray whitespace",
		Long: `Reads every secret from every readable store and reports two things.

Shared values. If two environments hold the same digest, one of them was almost
certainly populated by pasting the other's value. That is the mistake this whole
tool is built around: it is accepted by every store, passes every syntactic
check, and surfaces later as an authentication failure in production that names
nothing useful.

Damaged values. A stored value carrying a trailing newline or a control
character was written by something other than this tool — a web textarea, a
` + "`gh secret set X < file`" + `, an ` + "`echo`" + ` in a script. That newline is invisible in
every UI that displays a secret, so nothing else will ever show it to you.

Only digests are printed. No secret value reaches the terminal.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := loadSession()
			if err != nil {
				return err
			}

			ui.Title("%s › verify", s.cfg.Project)
			if err := s.preflight(ctx); err != nil {
				return err
			}

			var readable []store.Store
			for _, st := range s.stores {
				if st.Readable() {
					readable = append(readable, st)
				}
			}
			if len(readable) == 0 {
				ui.Blank()
				ui.Warn("no readable store is configured; GitHub Actions secrets are write-only,")
				ui.Warn("so there is nothing to read back. Configure gcp to make verify useful.")
				return nil
			}

			problems := 0
			for _, st := range readable {
				ui.Step("%s", st.Label())

				headers := append([]string{"SECRET"}, s.cfg.EnvNames()...)
				var rows [][]string

				for _, sec := range s.cfg.Secrets {
					if !sec.UsesStore(st.ID()) {
						continue
					}

					// digest -> the environments holding it, in declared order.
					byDigest := map[string][]string{}
					cells := map[string]string{}

					for _, e := range s.cfg.Environments {
						v, err := st.Read(ctx, sec, e)
						if err != nil {
							cells[e.Name] = ui.Absent()
							continue
						}
						fp := secretval.Fingerprint(v)
						byDigest[fp] = append(byDigest[fp], e.Name)
						cells[e.Name] = fp

						if damaged := describeDamage(v); damaged != "" {
							ui.Warn("%s in %s: %s", sec.Name, e.Name, damaged)
							problems++
						}
						if claims, bad := secretval.MarkerMismatch(sec, e.Name, v); bad {
							ui.Warn("%s in %s: the stored value is marked as %s",
								sec.Name, e.Name, claims)
							problems++
						}
					}

					for fp, envs := range byDigest {
						if len(envs) > 1 {
							ui.Warn("%s: %s share one value (%s)",
								sec.Name, strings.Join(envs, " and "), ui.Fingerprint(fp))
							problems++
						}
					}

					row := []string{sec.Name}
					for _, e := range s.cfg.Environments {
						row = append(row, cells[e.Name])
					}
					rows = append(rows, row)
				}

				ui.Blank()
				fmt.Println(ui.Indent(ui.Table(headers, rows), 2))
			}

			ui.Blank()
			if problems == 0 {
				ui.OK("no shared or damaged values found")
			} else {
				ui.Warn("%d problem(s) above; rotate the affected environments", problems)
			}
			ui.Note("matching digests in one row mean the same secret in two places")
			ui.Blank()
			return nil
		},
	}
}

// describeDamage names what is wrong with a stored value, without quoting it.
func describeDamage(v string) string {
	switch {
	case v == "":
		return "stored value is empty"
	case strings.HasSuffix(v, "\n"), strings.HasSuffix(v, "\r"):
		return "stored value ends in a newline — it was almost certainly written " +
			"by `echo` or a file redirect, and consumers will read the newline as " +
			"part of the credential"
	case strings.TrimSpace(v) != v:
		return "stored value has leading or trailing whitespace"
	case strings.ContainsAny(v, "\n\r\t"):
		return "stored value contains an interior newline or tab — likely a wrapped paste"
	}
	return ""
}

// verifyOne is kept for symmetry with rotate's per-secret handling; it exists so
// the damage rules have a single caller-visible entry point in tests.
func verifyOne(sec config.Secret, envName, v string) []string {
	var out []string
	if d := describeDamage(v); d != "" {
		out = append(out, d)
	}
	if claims, bad := secretval.MarkerMismatch(sec, envName, v); bad {
		out = append(out, "value is marked as "+claims)
	}
	return out
}
