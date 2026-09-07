package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimal = `
github:
  repo: ObsidianCodes/lsr
gcp:
  project: lifespanrecords
  prefix: lsr-
`

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".secretman.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMinimal(t *testing.T) {
	c, err := Load(write(t, minimal), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub.Repo != "ObsidianCodes/lsr" {
		t.Errorf("repo = %q", c.GitHub.Repo)
	}
	if c.GCP.Project != "lifespanrecords" || c.GCP.Prefix != "lsr-" {
		t.Errorf("gcp = %+v", c.GCP)
	}
}

// The config exists to be three fields. A secrets list here would be a second
// copy of what the stores already know, and the copy is the half that rots.
func TestSecretsListIsNotAField(t *testing.T) {
	_, err := Load(write(t, minimal+"\nsecrets:\n  - key: k\n    name: K\n"), "")
	if err == nil {
		t.Fatal("a secrets: block must be rejected, not quietly ignored")
	}
	if !strings.Contains(err.Error(), "secrets") {
		t.Errorf("the error should name the offending key, got %v", err)
	}
}

// A typo'd key is a silent misconfiguration if the decoder ignores it.
func TestUnknownFieldIsRejected(t *testing.T) {
	if _, err := Load(write(t, minimal+"\nnonsense: true\n"), ""); err == nil {
		t.Fatal("expected an unknown field to be rejected")
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"no store at all", "{}\n", "at least one store"},
		{"gcp without a project",
			"gcp:\n  prefix: lsr-\n", "gcp.project is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, tc.body), "")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

// github alone is a complete config: not every project uses Secret Manager.
func TestGitHubOnlyIsValid(t *testing.T) {
	if _, err := Load(write(t, "github:\n  repo: o/r\n"), ""); err != nil {
		t.Fatalf("github alone should be enough: %v", err)
	}
	// And an empty repo is legitimate: gh resolves it from the working directory.
	if _, err := Load(write(t, "github: {}\n"), ""); err != nil {
		t.Fatalf("an empty github block should be enough: %v", err)
	}
}

func TestDiscoverWalksUp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".secretman.yaml"),
		[]byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(deep)
	if err != nil {
		t.Fatalf("expected the config to be found by walking up: %v", err)
	}
	if filepath.Dir(got) != dir {
		t.Errorf("found %q, want the config in %q", got, dir)
	}
}

func TestFindReportsAbsenceWithoutError(t *testing.T) {
	got, err := Find(t.TempDir())
	if err != nil || got != "" {
		t.Fatalf(`Find on an empty tree = (%q, %v), want ("", nil)`, got, err)
	}
	if _, err := Discover(t.TempDir()); err == nil {
		t.Error("Discover on an empty tree returned no error")
	}
}
