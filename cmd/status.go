package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what exists in every store, and what one environment lacks",
		Long: `Lists what the stores hold, then compares the environments against each other.

The comparison is the useful half, and it costs nothing: if staging holds a
secret that production does not, that is either a deploy waiting to fail at its
next cold start or a leftover nobody needed. Either way it is worth seeing, and
it is derived entirely from what the stores report — there is no declared list
of what ought to exist, so there is nothing to keep in step and nothing to go
stale.

Presence only. No value is read here; for that, use verify.`,
		Example: `  secretman status`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, entries, err := gatherQuiet(ctx, "status")
			if err != nil {
				return err
			}

			for _, st := range s.stores {
				var mine []store.Entry
				for _, e := range entries {
					if e.Store == st.ID() {
						mine = append(mine, e)
					}
				}
				ui.Step("%s", st.Label())
				if len(mine) == 0 {
					ui.Note("nothing")
					continue
				}
				printStore(mine)
			}

			ui.Step("Environment differences")
			diffs := compareEnvironments(entries)
			if len(diffs) == 0 {
				ui.OK("every environment holds the same set of names")
			}
			for _, d := range diffs {
				ui.Warn("%s", d)
			}

			ui.Blank()
			ui.Note("presence only — `secretman verify` reads values back")
			ui.Blank()
			return nil
		},
	}
}

// printStore renders one store: a matrix when it has environments, a list when
// it does not.
func printStore(entries []store.Entry) {
	envs := environmentsOf(entries)
	if len(envs) == 0 {
		for _, e := range entries {
			ui.Note("%s", e.Name)
		}
		return
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	have := map[string]bool{}
	for _, e := range entries {
		have[e.Env+"\x00"+e.Name] = true
	}

	headers := append([]string{"SECRET"}, envs...)
	var rows [][]string
	for _, n := range sorted {
		row := []string{n}
		for _, env := range envs {
			if have[env+"\x00"+n] {
				row = append(row, ui.Present())
			} else {
				row = append(row, ui.Absent())
			}
		}
		rows = append(rows, row)
	}

	ui.Blank()
	fmt.Println(ui.Indent(ui.Table(headers, rows), 2))
}

// environmentsOf lists the environments present, with "" rendered as its own
// column because repository-wide is a real place a secret can be.
func environmentsOf(entries []store.Entry) []string {
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Env] = true
	}
	if len(seen) == 1 && seen[""] {
		return nil
	}
	var out []string
	for env := range seen {
		if env == "" {
			continue
		}
		out = append(out, env)
	}
	sort.Strings(out)
	if seen[""] {
		out = append([]string{"(repo-wide)"}, out...)
	}
	return out
}

// compareEnvironments reports names one environment holds and another does not.
//
// This is the closest thing to "is production fully configured?" that can be
// answered without a declared list, and it is arguably a better question: it
// catches drift in both directions, and it cannot be wrong about what ought to
// exist because it never claims to know.
func compareEnvironments(entries []store.Entry) []string {
	byStore := map[string]map[string]map[string]bool{} // store -> env -> names
	for _, e := range entries {
		if e.Env == "" {
			continue
		}
		if byStore[e.Store] == nil {
			byStore[e.Store] = map[string]map[string]bool{}
		}
		if byStore[e.Store][e.Env] == nil {
			byStore[e.Store][e.Env] = map[string]bool{}
		}
		byStore[e.Store][e.Env][e.Name] = true
	}

	var out []string
	for _, storeID := range sortedKeys(byStore) {
		envs := byStore[storeID]
		if len(envs) < 2 {
			continue
		}

		union := map[string]bool{}
		for _, names := range envs {
			for n := range names {
				union[n] = true
			}
		}

		for _, name := range sortedSet(union) {
			var missing []string
			for _, env := range sortedKeys(envs) {
				if !envs[env][name] {
					missing = append(missing, env)
				}
			}
			if len(missing) > 0 && len(missing) < len(envs) {
				out = append(out, fmt.Sprintf("%s: %s is missing from %s",
					storeID, name, strings.Join(missing, ", ")))
			}
		}
	}
	return out
}

// gatherQuiet is preflight plus enumeration, for the commands that only look.
func gatherQuiet(ctx context.Context, what string) (*session, []store.Entry, error) {
	s, err := loadSession()
	if err != nil {
		return nil, nil, err
	}
	ui.Title("secretman › %s", what)
	if err := s.preflight(ctx); err != nil {
		return nil, nil, err
	}
	entries, err := s.enumerate(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s, entries, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]bool) []string { return sortedKeys(m) }
