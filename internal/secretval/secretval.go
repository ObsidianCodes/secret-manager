// Package secretval sanitises and fingerprints secret values.
//
// What it does NOT do is judge whether a value is the right credential. Rules
// of that shape need a provider's current key format written down somewhere,
// which goes stale silently and then refuses a correct credential mid-rotation.
// Everything here works on any value from any provider and needs no
// configuration at all.
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
	"strings"
	"unicode"
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
