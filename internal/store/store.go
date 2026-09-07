// Package store reads and writes the places secrets live.
package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrWriteOnly is returned by Read on a store that cannot be read back.
// GitHub Actions secrets are the case: once written, nothing can retrieve them,
// which is why a rotation's only proof there is the digest of what was sent.
var ErrWriteOnly = errors.New("this store is write-only")

// ErrNotFound is returned by Read when the secret does not exist.
var ErrNotFound = errors.New("not found")

// Entry is one secret as a store actually holds it, right now.
//
// This is the unit everything works in, and it is deliberately not a
// "logical secret" that spans stores. Nothing here is inferred: the name is the
// name the store reports, and Env is empty when the store does not scope by
// environment. A tool that guessed which entries were "the same secret" would
// be guessing about the one thing it exists to be precise about.
type Entry struct {
	Store string // store ID: "github", "gcp"
	Name  string // exactly as the store holds it
	Env   string // "" when this store has no environment for it
}

// Label is what the operator sees, and the whole point of the walk: store,
// name as stored, environment when there is one.
func (e Entry) Label() string {
	if e.Env == "" {
		return fmt.Sprintf("%s:%s", e.Store, e.Name)
	}
	return fmt.Sprintf("%s:%s:%s", e.Store, e.Name, e.Env)
}

// ID is a stable key for an entry, used to track what a session has touched.
func (e Entry) ID() string { return e.Store + "\x00" + e.Env + "\x00" + e.Name }

// Store is one place secrets live.
type Store interface {
	// ID is the stable identifier used in output and flags.
	ID() string
	// Label is what the operator sees.
	Label() string

	// Preflight checks credentials and reachability before anything is
	// prompted for. A rotation that fails halfway leaves two stores disagreeing
	// about which credential is current, so everything checkable is checked
	// before the first keystroke.
	Preflight(ctx context.Context) error

	// Enumerate lists every secret this store currently holds for the project.
	// This is the only source of truth about what exists.
	Enumerate(ctx context.Context) ([]Entry, error)

	// Write stores a value. Implementations MUST pass the value on stdin and
	// MUST NOT append a trailing newline.
	Write(ctx context.Context, e Entry, value string) error

	// Read returns a stored value, or ErrWriteOnly, or ErrNotFound.
	Read(ctx context.Context, e Entry) (string, error)

	// Readable reports whether Read can ever succeed.
	Readable() bool

	// Environments lists the environments this store scopes secrets by. Empty
	// for a store that does not have the concept.
	Environments(ctx context.Context) ([]string, error)
}
