package cmd

import (
	"strings"
	"testing"

	"github.com/ObsidianCodes/secret-manager/internal/config"
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

func TestVerifyOneReportsBothProblems(t *testing.T) {
	sec := config.Secret{
		Name:       "WORKOS_API_KEY",
		Kind:       config.KindPrefixed,
		Prefix:     "sk_",
		EnvMarkers: map[string]string{"sk_live_": "production"},
	}

	// A production key stored in staging, with a trailing newline on top.
	problems := verifyOne(sec, "staging", "sk_live_abcdef\n")
	if len(problems) != 2 {
		t.Fatalf("expected the newline and the marker to be reported, got %v", problems)
	}

	if got := verifyOne(sec, "production", "sk_live_abcdef"); len(got) != 0 {
		t.Fatalf("expected no problems, got %v", got)
	}
}

func TestLastSegment(t *testing.T) {
	if got := lastSegment("production/WORKOS_API_KEY"); got != "WORKOS_API_KEY" {
		t.Fatalf("got %q", got)
	}
	if got := lastSegment("lsr-workos-api-key"); got != "lsr-workos-api-key" {
		t.Fatalf("got %q", got)
	}
}
