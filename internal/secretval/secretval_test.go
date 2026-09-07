package secretval

import (
	"strings"
	"testing"
)

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

// Sanitize must accept anything a provider might legitimately issue. It is the
// only gate a value passes through now, so a value it rejects is a value that
// cannot be rotated at all.
func TestSanitizeIsFormatAgnostic(t *testing.T) {
	for _, v := range []string{
		"sk_live_abc123",
		"ghp_16C7e42F292c6912E7710c838347Ae178B4a",
		"AIzaSyD-0123456789abcdefghijklmnopqrstu",
		"https://app.example.com/auth/callback",
		"x", // short, odd, and none of this tool's business
		"{\"type\":\"service_account\"}",
		"-----BEGIN PRIVATE KEY-----MIIEvQ...-----END PRIVATE KEY-----",
	} {
		got, _, err := Sanitize("ANY_NAME", v)
		if err != nil {
			t.Errorf("Sanitize refused a legitimate value shape (%q): %v", v, err)
		}
		if got != v {
			t.Errorf("Sanitize altered %q to %q", v, got)
		}
	}
}

func TestGenerateIsRandomAndLongEnough(t *testing.T) {
	v, err := Generate(0) // 0 means the default
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 32 {
		t.Fatalf("the default draw should base64 to at least 32 characters, got %d", len(v))
	}
	other, _ := Generate(0)
	if v == other {
		t.Fatal("two generated values collided")
	}
}
