package cmd

import (
	"strings"
	"testing"

	"github.com/ObsidianCodes/secret-manager/internal/store"
)

// A stored value ending in a newline is the failure verify exists to surface:
// no UI displays it, and every consumer reads it as part of the credential.
func TestDescribeDamage(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string // substring, or "" for undamaged
	}{
		{"clean", "sk_test_abcdef", ""},
		{"trailing newline", "sk_test_abcdef\n", "newline"},
		{"trailing carriage return", "sk_test_abcdef\r", "newline"},
		{"leading space", " sk_test_abcdef", "whitespace"},
		{"interior tab", "sk_test\tabcdef", "interior"},
		{"empty", "", "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeDamage(tc.value)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("expected undamaged, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected a message mentioning %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCompareEnvironmentsFindsWhatIsMissing(t *testing.T) {
	entries := []store.Entry{
		{Store: "github", Env: "production", Name: "API_KEY"},
		{Store: "github", Env: "production", Name: "CLIENT_ID"},
		{Store: "github", Env: "staging", Name: "API_KEY"},
		{Store: "github", Env: "staging", Name: "CLAIM_TOKEN"},
	}

	got := compareEnvironments(entries)
	joined := strings.Join(got, "\n")

	if !strings.Contains(joined, "CLIENT_ID is missing from staging") {
		t.Errorf("did not report the name staging lacks:\n%s", joined)
	}
	if !strings.Contains(joined, "CLAIM_TOKEN is missing from production") {
		t.Errorf("drift is worth reporting in both directions:\n%s", joined)
	}
	// Present everywhere is not news.
	if strings.Contains(joined, "API_KEY") {
		t.Errorf("reported a name that every environment holds:\n%s", joined)
	}
}

// One environment cannot differ from itself, and saying so would be noise on
// every repository that scopes nothing.
func TestCompareEnvironmentsStaysQuietWithOneEnvironment(t *testing.T) {
	entries := []store.Entry{
		{Store: "github", Env: "production", Name: "API_KEY"},
		{Store: "github", Name: "REPO_WIDE"},
	}
	if got := compareEnvironments(entries); len(got) != 0 {
		t.Errorf("expected nothing to report, got %v", got)
	}
}

// The label is what the operator reads instead of a validation rule, so it has
// to say all three things, and it must not invent an environment where the
// store has none.
func TestEntryLabel(t *testing.T) {
	if got := (store.Entry{Store: "github", Name: "API_KEY", Env: "staging"}).Label(); got != "github:API_KEY:staging" {
		t.Errorf("got %q", got)
	}
	if got := (store.Entry{Store: "gcp", Name: "lsr-api-key-staging"}).Label(); got != "gcp:lsr-api-key-staging" {
		t.Errorf("got %q", got)
	}
}

// Two entries that differ only by environment must be distinguishable, or a
// walk would treat them as one.
func TestEntryIDDistinguishesEnvironments(t *testing.T) {
	a := store.Entry{Store: "github", Name: "API_KEY", Env: "staging"}
	b := store.Entry{Store: "github", Name: "API_KEY", Env: "production"}
	c := store.Entry{Store: "gcp", Name: "API_KEY", Env: "staging"}
	if a.ID() == b.ID() || a.ID() == c.ID() {
		t.Error("entries in different places share an ID")
	}
	if a.ID() != (store.Entry{Store: "github", Name: "API_KEY", Env: "staging"}).ID() {
		t.Error("the same entry has an unstable ID")
	}
}

// --dry-run promises that nothing is created anywhere. init wrote the config
// regardless once, which is the one file this tool creates at all.
func TestInitWritesOnlyWhenNeitherFlagIsSet(t *testing.T) {
	cases := []struct {
		print, dryRun, write bool
	}{
		{false, false, true}, // an ordinary init
		{false, true, false}, // --dry-run
		{true, false, false}, // --print
		{true, true, false},  // both
	}
	for _, tc := range cases {
		if got := initWillWrite(tc.print, tc.dryRun); got != tc.write {
			t.Errorf("initWillWrite(print=%v, dryRun=%v) = %v, want %v",
				tc.print, tc.dryRun, got, tc.write)
		}
	}
}
