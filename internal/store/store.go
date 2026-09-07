// Package store writes secrets to the places that hold them.
package store

import (
	"context"
	"errors"

	"github.com/ObsidianCodes/secret-manager/internal/config"
)

// ErrWriteOnly is returned by Read on a store that cannot be read back.
// GitHub Actions secrets are the case: once written, nothing can retrieve them,
// which is why a rotation's only proof there is the digest of what was sent.
var ErrWriteOnly = errors.New("this store is write-only")

// ErrNotFound is returned by Read when the secret does not exist yet.
var ErrNotFound = errors.New("not found")

// Store is one place a secret lives.
type Store interface {
	// ID is the stable identifier used in config and flags.
	ID() string
	// Label is what the operator sees.
	Label() string

	// Preflight checks credentials and reachability before anything is
	// prompted for. A rotation that fails halfway leaves two stores disagreeing
	// about which credential is current, so everything checkable is checked
	// before the first keystroke.
	Preflight(ctx context.Context) error

	// Target is the fully-qualified name this secret has in this store, in this
	// environment. Shown in the confirmation, so the operator sees exactly what
	// is about to be overwritten.
	Target(s config.Secret, e config.Environment) string

	// List returns the names of the secrets that already exist for an
	// environment, so a create can be distinguished from an overwrite.
	List(ctx context.Context, e config.Environment) (map[string]bool, error)

	// Write stores a value. Implementations MUST pass the value on stdin and
	// MUST NOT append a trailing newline.
	Write(ctx context.Context, s config.Secret, e config.Environment, value string) error

	// Read returns a stored value, or ErrWriteOnly, or ErrNotFound.
	Read(ctx context.Context, s config.Secret, e config.Environment) (string, error)

	// Readable reports whether Read can ever succeed.
	Readable() bool
}

// Provisioner is implemented by stores that must create something before a
// secret can be written to it.
type Provisioner interface {
	// EnsureEnvironments creates whatever an environment-scoped write needs.
	EnsureEnvironments(ctx context.Context, envs []config.Environment, dryRun bool, log func(string, bool)) error
}
