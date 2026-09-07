// Package config loads the per-project coordinates.
//
// Coordinates, and nothing else: which repository, which GCP project, which
// name prefix. What secrets exist, and which environments hold them, is read
// from the stores at run time. A list kept here would be a second copy of
// something the stores already know, and the copy is the half that goes stale.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFilenames are looked for, in order, when --config is not given.
var DefaultFilenames = []string{".secretman.yaml", ".secretman.yml"}

// Config is where a project's secrets live. Three fields, on purpose.
type Config struct {
	GitHub *GitHub `yaml:"github,omitempty"`
	GCP    *GCP    `yaml:"gcp,omitempty"`

	// Path is where this config was loaded from. Not serialised.
	Path string `yaml:"-"`
}

// GitHub configures the GitHub Actions store.
type GitHub struct {
	// Repo is owner/name. Empty means "ask gh which repo we are in".
	Repo string `yaml:"repo,omitempty"`
}

// GCP configures the Google Secret Manager store.
type GCP struct {
	// Project is the project ID, which is not always the project NAME.
	Project string `yaml:"project"`
	// Prefix scopes this project's secrets within the GCP project, and is what
	// makes enumeration possible: everything carrying it is ours, everything
	// else is somebody else's and is left alone.
	Prefix string `yaml:"prefix,omitempty"`
}

// Load reads a config from path, or discovers one by walking up from dir.
func Load(path, dir string) (*Config, error) {
	if path == "" {
		found, err := Discover(dir)
		if err != nil {
			return nil, err
		}
		path = found
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // a typo'd key is a silent misconfiguration otherwise
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	c.Path = path

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Validate is exported because init builds a config in memory and must know it
// is writable before it writes it.
func (c *Config) Validate() error {
	if c.GitHub == nil && c.GCP == nil {
		return fmt.Errorf("configure at least one store (github, gcp)")
	}
	if c.GCP != nil && c.GCP.Project == "" {
		return fmt.Errorf("gcp.project is required when a gcp block is present")
	}
	return nil
}

// Discover walks up from dir looking for a config, and says what to do if there
// is none.
func Discover(dir string) (string, error) {
	found, err := Find(dir)
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no %s found here or in any parent directory\n"+
			"  run `secretman init` to create one, or pass --config <path>",
			DefaultFilenames[0])
	}
	return found, nil
}

// Find is Discover without the opinion: "" and no error means nothing found.
// init needs to tell "absent" from "broken" and act differently on each.
func Find(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	for {
		for _, name := range DefaultFilenames {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
