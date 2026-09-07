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
// next is usually not whoever ran init, and the two things they most need to
// know are that the file is meant to be committed and that it holds no value.
const Header = `# secretman — %s
#
# Written by ` + "`secretman init`" + `. Safe to commit, and meant to be: it names
# secrets, it never holds one. No value and no digest is ever written here.
#
# Edit with ` + "`secretman config add|edit|rm`" + `, or by hand — an unknown key is an
# error, not a shrug, so a typo cannot quietly do nothing.
#
# Reference:  secretman config schema
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
	return []byte(fmt.Sprintf(Header, c.Project) + "\n" + buf.String()), nil
}

// Save writes the config to path.
//
// Written through a temp file in the same directory and renamed, so an
// interrupted write leaves the previous config intact rather than a truncated
// one that no command can load.
func Save(c *Config, path string) error {
	if err := c.validate(); err != nil {
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

// DefaultPath is where init writes when nothing exists yet: the working
// directory, or the repository root if the working directory is inside one.
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

// SecretIndex reports where a secret lives in the slice, or -1.
func (c *Config) SecretIndex(key string) int {
	for i, s := range c.Secrets {
		if s.Key == key {
			return i
		}
	}
	return -1
}

// RemoveSecret drops a secret by key.
func (c *Config) RemoveSecret(key string) bool {
	i := c.SecretIndex(key)
	if i < 0 {
		return false
	}
	c.Secrets = append(c.Secrets[:i], c.Secrets[i+1:]...)
	return true
}

// PutSecret adds a secret, or replaces the one with the same key.
func (c *Config) PutSecret(s Secret) {
	if i := c.SecretIndex(s.Key); i >= 0 {
		c.Secrets[i] = s
		return
	}
	c.Secrets = append(c.Secrets, s)
}
