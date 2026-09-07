package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimal = `
project: lsr
github:
  repo: ObsidianCodes/lsr
gcp:
  project: lifespanrecords
  prefix: lsr-
environments:
  - name: production
    gcpSuffix: ""
  - name: staging
    gcpSuffix: "-staging"
secrets:
  - key: workos-api-key
    name: WORKOS_API_KEY
    help: Dashboard › API Keys
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
	if c.Project != "lsr" {
		t.Fatalf("got %q", c.Project)
	}
	if got := c.EnvNames(); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

// An explicit empty gcpSuffix is production's, and must not be replaced by the
// "-<name>" default: the name most consumers reference should carry no suffix.
func TestExplicitEmptySuffixSurvives(t *testing.T) {
	c, err := Load(write(t, minimal), "")
	if err != nil {
		t.Fatal(err)
	}
	prod, _ := c.Env("production")
	if prod.Suffix() != "" {
		t.Fatalf("expected production to be unsuffixed, got %q", prod.Suffix())
	}
	stg, _ := c.Env("staging")
	if stg.Suffix() != "-staging" {
		t.Fatalf("got %q", stg.Suffix())
	}
}

func TestOmittedSuffixDefaultsToTheEnvironmentName(t *testing.T) {
	c, err := Load(write(t, strings.Replace(minimal,
		"  - name: staging\n    gcpSuffix: \"-staging\"",
		"  - name: staging", 1)), "")
	if err != nil {
		t.Fatal(err)
	}
	stg, _ := c.Env("staging")
	if stg.Suffix() != "-staging" {
		t.Fatalf("got %q", stg.Suffix())
	}
}

func TestLabelDefaultsToName(t *testing.T) {
	c, err := Load(write(t, minimal), "")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Secret("workos-api-key")
	if s.Label != s.Name {
		t.Fatalf("label = %q, want the name %q", s.Label, s.Name)
	}
}

func TestGitHubEnvDefaultsToName(t *testing.T) {
	c, _ := Load(write(t, minimal), "")
	e, _ := c.Env("staging")
	if e.GitHubEnv != "staging" {
		t.Fatalf("got %q", e.GitHubEnv)
	}
}

// A typo'd key is a silent misconfiguration if the decoder ignores it, and a
// silent misconfiguration in this tool means a secret quietly not validated.
func TestUnknownFieldIsRejected(t *testing.T) {
	_, err := Load(write(t, minimal+"\nnonsense: true\n"), "")
	if err == nil {
		t.Fatal("expected an unknown field to be rejected")
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"no project", strings.Replace(minimal, "project: lsr", "", 1), "project is required"},
		{"no name",
			strings.Replace(minimal, "    name: WORKOS_API_KEY\n", "", 1), "has no name"},
		{"unknown store",
			minimal + "    stores: [vault]\n", `unknown store "vault"`},
		{"no store configured",
			strings.Replace(strings.Replace(minimal,
				"github:\n  repo: ObsidianCodes/lsr\n", "", 1),
				"gcp:\n  project: lifespanrecords\n  prefix: lsr-\n", "", 1),
			"configure at least one store"},
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

func TestDiscoverWalksUp(t *testing.T) {
	p := write(t, minimal)
	deep := filepath.Join(filepath.Dir(p), "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := Load("", deep)
	if err != nil {
		t.Fatalf("expected the config to be found by walking up: %v", err)
	}
	if c.Project != "lsr" {
		t.Fatalf("got %q", c.Project)
	}
}

func TestUsesStore(t *testing.T) {
	all := Secret{}
	if !all.UsesStore("github") || !all.UsesStore("gcp") {
		t.Fatal("an empty stores list means every store")
	}
	only := Secret{Stores: []string{"gcp"}}
	if only.UsesStore("github") || !only.UsesStore("gcp") {
		t.Fatal("stores did not restrict")
	}
}
