package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writable() *Config {
	empty := ""
	return &Config{
		Project: "lsr",
		GitHub:  &GitHub{Repo: "ObsidianCodes/lsr"},
		GCP:     &GCP{Project: "lifespanrecords", Prefix: "lsr-"},
		Environments: []Environment{
			{Name: "production", GCPSuffix: &empty},
			{Name: "staging"},
		},
		Secrets: []Secret{{
			Key:  "workos-api-key",
			Name: "WORKOS_API_KEY",
			Help: "Dashboard › API Keys",
		}},
	}
}

func TestSaveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".secretman.yaml")
	if err := Save(writable(), path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Project != "lsr" || len(got.Secrets) != 1 {
		t.Fatalf("round trip lost content: %+v", got)
	}
	if got.Secrets[0].Help != "Dashboard › API Keys" {
		t.Errorf("help lost: %q", got.Secrets[0].Help)
	}
	// An explicit empty suffix is production's whole point, and it is the one
	// field a naive round trip turns back into "-production".
	if s := got.Environments[0].Suffix(); s != "" {
		t.Errorf("production suffix became %q; explicit empty must survive", s)
	}
	if s := got.Environments[1].Suffix(); s != "-staging" {
		t.Errorf("staging suffix = %q, want -staging", s)
	}
}

// A generated config that is mostly empty keys is a config nobody can read, and
// the wizard leaves most fields unset.
func TestSaveOmitsEmptyFields(t *testing.T) {
	out, err := Marshal(writable())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"label:", "stores:", "githubEnv:"} {
		if strings.Contains(string(out), key) {
			t.Errorf("unset field %q was written out:\n%s", key, out)
		}
	}
}

func TestSaveRefusesInvalidConfig(t *testing.T) {
	c := writable()
	c.Secrets = nil // a config with no secrets does not load
	path := filepath.Join(t.TempDir(), ".secretman.yaml")

	if err := Save(c, path); err == nil {
		t.Fatal("Save accepted a config that Load would reject")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused Save still created the file")
	}
}

// Save must not destroy the previous config when the new one cannot be written.
func TestSaveLeavesPreviousFileOnRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".secretman.yaml")
	if err := Save(writable(), path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	bad := writable()
	bad.Project = ""
	_ = Save(bad, path)

	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a refused Save modified the existing config")
	}
}

func TestLoadRawKeepsDefaultsOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".secretman.yaml")
	if err := os.WriteFile(path, []byte(
		"project: lsr\n"+
			"github:\n  repo: o/r\n"+
			"environments:\n  - name: staging\n"+
			"secrets:\n  - key: k\n    name: K\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadRaw(path, "")
	if err != nil {
		t.Fatalf("LoadRaw: %v", err)
	}
	if raw.Secrets[0].Label != "" || raw.Environments[0].GCPSuffix != nil {
		t.Errorf("LoadRaw applied defaults: %+v", raw.Secrets[0])
	}

	full, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if full.Secrets[0].Label != "K" {
		t.Errorf("Load did not default the label: %q", full.Secrets[0].Label)
	}
}

func TestFindReportsAbsenceWithoutError(t *testing.T) {
	got, err := Find(t.TempDir())
	if err != nil || got != "" {
		t.Fatalf("Find on an empty tree = (%q, %v), want (\"\", nil)", got, err)
	}
	if _, err := Discover(t.TempDir()); err == nil {
		t.Error("Discover on an empty tree returned no error")
	}
}
