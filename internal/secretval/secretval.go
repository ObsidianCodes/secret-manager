// Package secretval sanitises, validates and fingerprints secret values.
//
// Nothing here ever returns a secret in an error message. An error says what is
// wrong with a value, never what the value is: errors are printed, and printed
// things end up in scrollback, screenshots and pasted bug reports.
package secretval

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/ObsidianCodes/secret-manager/internal/config"
)

// Fingerprint is a short digest, so two values can be compared - and a written
// value verified - without either of them reaching the terminal or a log.
//
// Twelve hex characters of SHA-256. Short enough to read aloud, far too short
// to attack, and it only ever confirms an equality the operator already
// suspects.
func Fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// Generate draws a fresh random value.
func Generate(nbytes int) (string, error) {
	if nbytes <= 0 {
		nbytes = 32
	}
	buf := make([]byte, nbytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	// base64 rather than hex: same entropy in two thirds the characters, and
	// every byte is printable ASCII, which is what the stores accept.
	return base64.StdEncoding.EncodeToString(buf), nil
}

// Sanitize strips the damage a copy-paste routinely does, and rejects the
// damage it cannot safely undo. The returned notes describe what was stripped,
// so the operator sees that the value they typed is not quite the value that
// will be written.
func Sanitize(name, raw string) (clean string, notes []string, err error) {
	v := raw

	// 1. Surrounding whitespace, including the trailing newline that this whole
	//    package exists to prevent. A trailing \n is invisible in every UI that
	//    displays a secret, so it costs far more to diagnose than to strip.
	v = strings.TrimSpace(v)

	// 2. A value pasted straight out of a .env file arrives as NAME=value.
	if name != "" && strings.HasPrefix(v, name+"=") {
		v = strings.TrimPrefix(v, name+"=")
		notes = append(notes, "stripped a leading "+name+"= (pasted from .env?)")
	}

	// 3. .env quotes its values; the quotes are not part of the secret.
	for _, q := range []string{`"`, `'`} {
		if len(v) >= 2 && strings.HasPrefix(v, q) && strings.HasSuffix(v, q) {
			v = v[1 : len(v)-1]
			notes = append(notes, "stripped surrounding "+q+" quotes")
			break
		}
	}

	// 4. Anything left that is not printable ASCII means the paste is mangled -
	//    most often a terminal that hard-wrapped a long key. Repairing that by
	//    guessing would write a silently truncated credential, which fails at
	//    the next cold start rather than here.
	for _, r := range v {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return "", notes, fmt.Errorf(
				"contains a newline, tab, or control character\n" +
					"a wrapped paste is usually mangled: copy the value as a single line")
		}
	}

	if v == "" {
		return "", notes, fmt.Errorf("empty")
	}
	return v, notes, nil
}

// Validate applies the rules specific to one secret in one environment.
func Validate(s config.Secret, envName, v string) error {
	if s.MinLength > 0 && len(v) < s.MinLength {
		return fmt.Errorf("must be at least %d characters (got %d)", s.MinLength, len(v))
	}

	// A conflicting prefix is checked before the required one, so a transposed
	// paste is told what it actually is rather than what it is not.
	for prefix, what := range s.Conflicts {
		if strings.HasPrefix(v, prefix) {
			return fmt.Errorf("this is %s (%s...), not %s", what, prefix, s.Name)
		}
	}

	switch s.Kind {
	case config.KindPrefixed:
		if !strings.HasPrefix(v, s.Prefix) {
			return fmt.Errorf("%s starts with %q", s.Name, s.Prefix)
		}
		if s.MinLength == 0 && len(v) < 16 {
			return fmt.Errorf("suspiciously short (%d characters)", len(v))
		}

	case config.KindURL:
		u, err := url.Parse(v)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("not an absolute URL (expected https://...)")
		}
		if strings.ContainsAny(v, " \t") {
			return fmt.Errorf("contains a space")
		}
		switch u.Scheme {
		case "https":
		case "http":
			if !allowed(s.AllowInsecureIn, envName) {
				return fmt.Errorf("http:// is only acceptable in %s",
					orNone(s.AllowInsecureIn))
			}
		default:
			return fmt.Errorf("scheme %q is not http or https", u.Scheme)
		}

	case config.KindPassword:
		if s.MinLength == 0 && len(v) < 16 {
			return fmt.Errorf("suspiciously short (%d characters)", len(v))
		}

	case config.KindOpaque:
		if s.MinLength == 0 && len(v) < 8 {
			return fmt.Errorf("suspiciously short (%d characters)", len(v))
		}
	}
	return nil
}

// MarkerMismatch reports an environment marker embedded in the value that
// contradicts the environment being rotated.
//
// Some providers say which environment a credential came from in the value
// itself - a live key and a test key differ by their prefix. Pasting the live
// key into staging fails neither loudly nor immediately: it is accepted, and
// the staging app then reads and writes real production data. That is the
// transposition worth catching, and unlike a cross-store comparison it needs no
// network call and works on a first-time setup where nothing is stored yet.
//
// Returns the environment the value claims to belong to, and whether it
// contradicts envName.
func MarkerMismatch(s config.Secret, envName, v string) (claims string, mismatch bool) {
	for prefix, belongs := range s.EnvMarkers {
		if strings.HasPrefix(v, prefix) {
			return belongs, belongs != envName
		}
	}
	return "", false
}

// Shape renders a value for the screen with its body blanked, keeping only the
// prefix - the part that says which environment it came from.
func Shape(s config.Secret, v string) string {
	for prefix := range s.EnvMarkers {
		if strings.HasPrefix(v, prefix) {
			return prefix + "…"
		}
	}
	if s.Prefix != "" && strings.HasPrefix(v, s.Prefix) {
		return s.Prefix + "…"
	}
	if s.Kind == config.KindURL {
		return v // a redirect URI is not a secret; showing it is the point
	}
	return fmt.Sprintf("%d characters", len(v))
}

func allowed(list []string, env string) bool {
	for _, e := range list {
		if e == env {
			return true
		}
	}
	return false
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "no environment"
	}
	return strings.Join(list, ", ")
}
