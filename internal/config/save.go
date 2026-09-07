package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Header is prepended to every generated config.
//
// It is a comment, not documentation for its own sake: whoever opens this file
// next is usually not whoever ran init, and what they need to know is that the
// file is only coordinates — it lists no secret, so it cannot be out of date
// about one.
const Header = `# secretman — where this project's secrets live.
#
# Written by ` + "`secretman init`" + `. Safe to commit, and meant to be.
#
# This file holds coordinates, not contents: no secret name, no value, no
# digest. What exists is read from the stores themselves every run, so this
# file cannot go stale about it.
#
# Changing repo or prefix points secretman at a different set of secrets. The
# ones it points at now keep existing and stop being visible to it.
`

// Marshal renders a config as the bytes that belong on disk.
func Marshal(c *Config) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(Header + "\n" + buf.String()), nil
}

// Save writes the config to path.
//
// Written through a temp file in the same directory and renamed, so an
// interrupted write leaves the previous config intact rather than a truncated
// one that no command can load.
func Save(c *Config, path string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("refusing to write an invalid config: %w", err)
	}
	out, err := Marshal(c)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".secretman-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds

	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// DefaultPath is where init writes when nothing exists yet: the repository
// root, or the working directory if it is not inside one.
func DefaultPath(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	if root := gitRoot(dir); root != "" {
		dir = root
	}
	return filepath.Join(dir, DefaultFilenames[0]), nil
}

// gitRoot walks up looking for a .git entry. Shelling out to git would be more
// correct in a worktree, but init must work in a directory that is not a
// repository at all, and a missing root is not an error here.
func gitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
