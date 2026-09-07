package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/secretval"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Read stored values back and report shared or damaged ones",
		Long: `Reads every secret from every readable store and reports two things.

Shared values. If two entries hold the same digest, the same credential is in
both places. Sometimes that is correct and deliberate — one credential written
to two stores is exactly what a rotation does. Sometimes it means production's
key was pasted into staging, which every store accepts and only production
notices. This says which entries match and leaves the judgement to you.

Damaged values. A stored value carrying a trailing newline or a control
character was written by something other than this tool — a web textarea, a
` + "`gh secret set X < file`" + `, an ` + "`echo`" + ` in a script. That newline is invisible in
every UI that displays a secret, so nothing else will ever show it to you.

Only digests are printed. No secret value reaches the terminal.

GitHub Actions secrets are write-only, so a project on GitHub alone gets an
empty report rather than a clean bill of health, and says so.`,
		Example: `  secretman verify`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, entries, err := gatherQuiet(ctx, "verify")
			if err != nil {
				return err
			}

			var readable []store.Entry
			for _, e := range entries {
				if st := s.storeFor(e); st != nil && st.Readable() {
					readable = append(readable, e)
				}
			}
			if len(readable) == 0 {
				ui.Blank()
				ui.Warn("nothing readable is configured; GitHub Actions secrets are write-only,")
				ui.Warn("so there is nothing to read back. Add a gcp store to make verify useful.")
				ui.Blank()
				return nil
			}

			ui.Step("Reading")
			problems := 0
			byDigest := map[string][]string{}

			for _, e := range readable {
				v, err := s.storeFor(e).Read(ctx, e)
				if err != nil {
					ui.Warn("%s: could not be read: %v", e.Label(), err)
					problems++
					continue
				}
				fp := secretval.Fingerprint(v)
				byDigest[fp] = append(byDigest[fp], e.Label())

				if damaged := describeDamage(v); damaged != "" {
					ui.Warn("%s: %s", e.Label(), damaged)
					problems++
					continue
				}
				ui.OK("%s — %s", e.Label(), ui.Fingerprint(fp))
			}

			ui.Step("Shared values")
			shared := 0
			for _, fp := range sortedKeys(byDigest) {
				where := byDigest[fp]
				if len(where) > 1 {
					ui.Warn("%s share one value (%s)",
						strings.Join(where, " and "), ui.Fingerprint(fp))
					shared++
				}
			}
			if shared == 0 {
				ui.OK("every readable secret holds a distinct value")
			}

			ui.Blank()
			if problems == 0 {
				ui.OK("no damaged values found")
			} else {
				ui.Warn("%d problem(s) above", problems)
			}
			ui.Note("a shared value is only a problem if those places should differ")
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
