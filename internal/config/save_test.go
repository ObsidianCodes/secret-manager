package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writable() *Config {
	return &Config{
		GitHub: &GitHub{Repo: "ObsidianCodes/lsr"},
		GCP:    &GCP{Project: "lifespanrecords", Prefix: "lsr-"},
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
	if got.GitHub.Repo != "ObsidianCodes/lsr" ||
		got.GCP.Project != "lifespanrecords" || got.GCP.Prefix != "lsr-" {
		t.Fatalf("round trip lost content: %+v %+v", got.GitHub, got.GCP)
	}
}

// The header is the only thing most people will read about this file, so it has
// to say the two things that matter: commit it, and it holds no secret.
func TestSavedFileExplainsItself(t *testing.T) {
	out, err := Marshal(writable())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Safe to commit", "no secret name, no value"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the header no longer says %q:\n%s", want, out)
		}
	}
}

func TestSaveRefusesInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".secretman.yaml")
	if err := Save(&Config{}, path); err == nil {
		t.Fatal("Save accepted a config that Load would reject")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a refused Save still created the file")
	}
}

// A failed write must not take the working config with it.
func TestSaveLeavesPreviousFileOnRefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".secretman.yaml")
	if err := Save(writable(), path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	_ = Save(&Config{GCP: &GCP{Prefix: "no-project-"}}, path)

	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("a refused Save modified the existing config")
	}
}
