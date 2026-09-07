package cmd

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

// newDeleteCmd builds the delete command under the given name, so the same
// command can live at the top level and stay reachable at its old address
// under config.
func newDeleteCmd(use string) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:     use,
		Aliases: aliasesFor(use),
		Short:   "Delete secrets from the stores, permanently",
		Long: `Lists everything the stores hold, deletes what you tick off.

This destroys the secret itself. There is no local list to remove something
from, so the only thing there is to delete is the credential, in the store, for
real. In Secret Manager that takes every version with it — nothing is left to
roll back to. Anything still reading it fails at its next cold start.

It is not recoverable. So it asks twice: once with the list of exactly what will
go, and once to confirm, defaulting to no.

If the aim is to stop rotating something rather than to destroy it, there is
nothing to do here: rotate walks what exists, and Enter leaves it as is.`,
		Example: `  secretman delete
  secretman delete --dry-run     # show what would go, delete nothing
  secretman delete --force       # skip the confirmation`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), force)
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false,
		"skip the confirmation (the list is still shown)")
	return c
}

func aliasesFor(use string) []string {
	if use == "delete" {
		return []string{"rm", "remove"}
	}
	return []string{"remove", "delete"}
}

func runDelete(ctx context.Context, force bool) error {
	if err := requireTTY("delete"); err != nil {
		return err
	}
	s, entries, err := gatherQuiet(ctx, "delete")
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		ui.Blank()
		ui.Note("Nothing exists in any configured store.")
		ui.Blank()
		return nil
	}

	chosen, err := pickEntries(entries, "Delete which secrets?",
		"this destroys them in the store — it does not merely unmanage them")
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		ui.Blank()
		ui.Note("Nothing selected.")
		ui.Blank()
		return nil
	}

	// Exactly what will go, listed again outside the picker, because a ticked
	// checkbox scrolled off the top of a list is not the same as reading it.
	ui.Step("About to delete")
	for _, e := range chosen {
		ui.Warn("%s", e.Label())
	}
	ui.Blank()
	ui.Warn("This is NOT recoverable.")
	ui.Warn("  Every version goes with it. There is no rollback afterwards, and")
	ui.Warn("  anything still reading these fails at its next cold start.")
	ui.Blank()

	if flagDryRun {
		ui.OK("dry run complete; nothing deleted")
		ui.Blank()
		return nil
	}

	if !force {
		// Defaults to no. The affirmative here is unrecoverable, so it should
		// take a deliberate movement rather than a reflex Enter.
		confirmed := false
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Permanently delete %d secret(s)?", len(chosen))).
				Description("this cannot be undone · default is no").
				Affirmative("Yes, delete them").
				Negative("No, keep them").
				Value(&confirmed),
		)).WithTheme(theme()).Run(); err != nil {
			return err
		}
		if !confirmed {
			ui.Blank()
			ui.Note("Nothing deleted.")
			ui.Blank()
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
	ui.Note("if these were live credentials, revoke them at their source too")
	ui.Blank()
	return nil
}
