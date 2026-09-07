package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// theme is the one huh theme the whole CLI uses.
func theme() *huh.Theme { return huh.ThemeCharm() }

// requireTTY refuses to start a form that cannot be answered. A form on a pipe
// otherwise fails deep inside bubbletea with an error naming a terminal mode
// rather than the thing the operator actually needs to do.
func requireTTY(what string) error {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	return fmt.Errorf("%s needs a terminal; run it interactively", what)
}

func notBlank(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", what)
		}
		return nil
	}
}

// optional turns a validator into one that accepts an empty answer.
func optional(f func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return f(s)
	}
}

func looksLikeRepo(s string) error {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("must be owner/name")
	}
	return nil
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
