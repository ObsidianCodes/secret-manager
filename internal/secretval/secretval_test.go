package secretval

import (
	"strings"
	"testing"

	"github.com/ObsidianCodes/secret-manager/internal/config"
)

func apiKey() config.Secret {
	return config.Secret{
		Key:       "workos-api-key",
		Name:      "WORKOS_API_KEY",
		Kind:      config.KindPrefixed,
		Prefix:    "sk_",
		MinLength: 20,
		Conflicts: map[string]string{"client_": "a client id"},
		EnvMarkers: map[string]string{
			"sk_live_": "production",
		},
	}
}

func redirectURI() config.Secret {
	return config.Secret{
		Key:             "workos-redirect-uri",
		Name:            "WORKOS_REDIRECT_URI",
		Kind:            config.KindURL,
		AllowInsecureIn: []string{"development"},
	}
}

// The trailing newline is the bug the whole tool exists to prevent, so it gets
// the first test.
func TestSanitizeStripsTrailingNewline(t *testing.T) {
	got, _, err := Sanitize("WORKOS_API_KEY", "sk_test_abcdefghijklmnop\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("trailing newline survived sanitize")
	}
	if got != "sk_test_abcdefghijklmnop" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeStripsEnvFileDecoration(t *testing.T) {
	got, notes, err := Sanitize("WORKOS_API_KEY", `WORKOS_API_KEY="sk_test_abcdefghijklmnop"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sk_test_abcdefghijklmnop" {
		t.Fatalf("got %q", got)
	}
	if len(notes) != 2 {
		t.Fatalf("expected the name= and the quotes to be reported, got %v", notes)
	}
}

// An interior newline means a wrapped paste. Repairing it by guessing would
// write a silently truncated credential, so it must be refused, not fixed.
func TestSanitizeRefusesInteriorNewline(t *testing.T) {
	if _, _, err := Sanitize("X", "sk_test_abcdef\nghijklmnop"); err == nil {
		t.Fatal("expected an interior newline to be refused")
	}
}

func TestSanitizeNeverLeaksTheValue(t *testing.T) {
	secret := "sk_test_SUPERSECRETVALUE\ttab"
	_, _, err := Sanitize("X", secret)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatalf("the error quoted the secret: %q", err)
	}
}

func TestValidatePrefixAndConflicts(t *testing.T) {
	s := apiKey()
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{"good key", "sk_test_abcdefghijklmnopqr", true},
		{"client id in the key field", "client_abcdefghijklmnopqrst", false},
		{"no prefix", "abcdefghijklmnopqrstuvwx", false},
		{"too short", "sk_test_a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(s, "staging", tc.value)
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected invalid")
			}
		})
	}
}

// The transposition message must name what the value actually is; "wrong
// prefix" sends the operator back to the same two adjacent dashboard fields
// with no idea which one they misread.
func TestValidateNamesTheTransposition(t *testing.T) {
	err := Validate(apiKey(), "staging", "client_abcdefghijklmnopqrst")
	if err == nil || !strings.Contains(err.Error(), "a client id") {
		t.Fatalf("expected the error to name the value, got %v", err)
	}
}

func TestValidateURLPerEnvironment(t *testing.T) {
	s := redirectURI()
	if err := Validate(s, "development", "http://localhost:4200/auth/callback"); err != nil {
		t.Fatalf("localhost should be fine in development: %v", err)
	}
	if err := Validate(s, "production", "http://localhost:4200/auth/callback"); err == nil {
		t.Fatal("localhost must be refused in production")
	}
	if err := Validate(s, "production", "https://app.example.com/auth/callback"); err != nil {
		t.Fatalf("https should be fine anywhere: %v", err)
	}
	if err := Validate(s, "production", "app.example.com/auth/callback"); err == nil {
		t.Fatal("a relative URL must be refused")
	}
}

// A live key in staging is accepted by every store and fails only in
// production, so it is refused here without any network call.
func TestMarkerMismatch(t *testing.T) {
	s := apiKey()

	claims, bad := MarkerMismatch(s, "staging", "sk_live_abcdefghijklmnop")
	if !bad || claims != "production" {
		t.Fatalf("expected a production marker mismatch, got %q %v", claims, bad)
	}

	if _, bad := MarkerMismatch(s, "production", "sk_live_abcdefghijklmnop"); bad {
		t.Fatal("a live key in production is correct")
	}

	// An unmarked value says nothing about its environment, and must not be
	// guessed at.
	if _, bad := MarkerMismatch(s, "production", "sk_test_abcdefghijklmnop"); bad {
		t.Fatal("sk_test_ has no marker configured, so it cannot mismatch")
	}
}

func TestFingerprintIsStableAndShort(t *testing.T) {
	a := Fingerprint("hello")
	if a != Fingerprint("hello") {
		t.Fatal("not stable")
	}
	if a == Fingerprint("hello\n") {
		t.Fatal("a trailing newline must change the digest; that is the point")
	}
	if len(a) != 12 {
		t.Fatalf("expected 12 characters, got %d", len(a))
	}
}

func TestGenerateMeetsMinimumLength(t *testing.T) {
	v, err := Generate(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 32 {
		t.Fatalf("32 random bytes should base64 to at least 32 characters, got %d", len(v))
	}
	other, _ := Generate(32)
	if v == other {
		t.Fatal("two generated values collided")
	}
}

func TestShapeNeverRevealsTheBody(t *testing.T) {
	got := Shape(apiKey(), "sk_live_THISISTHESECRET")
	if strings.Contains(got, "THISISTHESECRET") {
		t.Fatalf("shape leaked the value: %q", got)
	}
	if !strings.HasPrefix(got, "sk_live_") {
		t.Fatalf("shape should keep the environment marker, got %q", got)
	}
}
