package cmd

import (
	"reflect"
	"testing"

	"github.com/ObsidianCodes/secret-manager/internal/config"
)

func TestDeriveKey(t *testing.T) {
	for in, want := range map[string]string{
		"WORKOS_API_KEY": "workos-api-key",
		"  DATABASE_URL": "database-url",
		"already-a-key":  "already-a-key",
	} {
		if got := deriveKey(in); got != want {
			t.Errorf("deriveKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretStoresExpandsEmptyToAll(t *testing.T) {
	c := &config.Config{GitHub: &config.GitHub{}, GCP: &config.GCP{}}
	if got := secretStores(c, config.Secret{}); !reflect.DeepEqual(got, []string{"github", "gcp"}) {
		t.Errorf("secretStores = %v, want both stores", got)
	}
	if got := secretStores(c, config.Secret{Stores: []string{"gcp"}}); !reflect.DeepEqual(got, []string{"gcp"}) {
		t.Errorf("secretStores = %v, want [gcp]", got)
	}
}
