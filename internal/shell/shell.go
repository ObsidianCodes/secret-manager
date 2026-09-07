// Package shell runs the external CLIs the stores are built on (gh, gcloud).
//
// Shelling out rather than using the vendored SDKs is deliberate: both CLIs
// already hold the operator's credentials, and reusing them means this tool
// never asks for, stores, or refreshes a token of its own.
//
// The one rule every call here obeys: a secret value is passed on STDIN, never
// in an argument. Arguments are visible in `ps` to every other user on the
// machine, and they end up in shell history when a command is reconstructed by
// hand from an error message.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrMissing is returned when the binary is not on PATH.
var ErrMissing = errors.New("not installed or not on PATH")

// Have reports whether a binary is available.
func Have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Require returns a helpful error when a binary is missing.
func Require(name string) error {
	if !Have(name) {
		return fmt.Errorf("%s is %w", name, ErrMissing)
	}
	return nil
}

// Result is the outcome of one command.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Run executes name with args and no stdin.
func Run(ctx context.Context, name string, args ...string) (Result, error) {
	return RunStdin(ctx, "", name, args...)
}

// RunStdin executes name with args, writing stdin to the process. Use this,
// never an argument, for anything secret.
func RunStdin(ctx context.Context, stdin string, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	if stdin != "" {
		// A bytes.Reader over the exact bytes: no trailing newline is added
		// here, and none may be added anywhere else either. An invisible \n on
		// a redirect URI or a connection string is the single most expensive
		// class of bug this whole tool exists to prevent.
		cmd.Stdin = bytes.NewReader([]byte(stdin))
	}

	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String()}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.Code = ee.ExitCode()
		return res, fmt.Errorf("%s %s: exit %d: %s",
			name, strings.Join(args, " "), res.Code, firstLine(res.Stderr))
	}
	if err != nil {
		return res, fmt.Errorf("%s: %w", name, err)
	}
	return res, nil
}

// Quiet runs a command purely for its exit status.
func Quiet(ctx context.Context, name string, args ...string) bool {
	_, err := Run(ctx, name, args...)
	return err == nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no output)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
