// Package config loads the per-project secret specification.
//
// The bash script this replaces hardcoded one project's secrets in parallel
// arrays. Everything that was hardcoded lives here instead, so the same binary
// rotates LSR, GLOME, or anything else by pointing at a different file.
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

// Config is a whole project's secret specification.
type Config struct {
	Project      string        `yaml:"project"`
	GitHub       *GitHub       `yaml:"github,omitempty"`
	GCP          *GCP          `yaml:"gcp,omitempty"`
	Environments []Environment `yaml:"environments"`
	Secrets      []Secret      `yaml:"secrets"`

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
	// Prefix is prepended to every secret name, so several projects can share
	// one GCP project without colliding.
	Prefix string `yaml:"prefix,omitempty"`
}

// Environment is one deployment target.
type Environment struct {
	Name string `yaml:"name"`
	// GitHubEnv is the GitHub Environment name. Defaults to Name.
	GitHubEnv string `yaml:"githubEnv,omitempty"`
	// GCPSuffix distinguishes this environment's Secret Manager names.
	// Secret Manager has no concept of environments, so it goes in the name.
	// The empty string is legitimate, and conventionally means production.
	GCPSuffix *string `yaml:"gcpSuffix"`
}

// Secret is one credential, in every environment.
//
// There is deliberately nothing here describing what a valid value looks like.
// Rules of that shape — a required prefix, a minimum length, a marker saying
// which environment a value came from — encode a provider's current format into
// a file nobody maintains, and their failure mode is refusing a correct
// credential at the moment somebody is trying to rotate it. What replaces them
// is showing the operator precisely which store, environment and name they are
// about to overwrite, before they type anything.
type Secret struct {
	// Key is the stable identifier used by --only and in output.
	Key string `yaml:"key"`
	// Name is the environment variable / GitHub secret name.
	Name string `yaml:"name"`
	// Label is what the prompt says.
	Label string `yaml:"label,omitempty"`
	// Help is the dimmed line under the prompt: where to find this value.
	Help string `yaml:"help,omitempty"`
	// Stores restricts this secret to some stores by ID ("github", "gcp").
	// Empty means every configured store.
	Stores []string `yaml:"stores,omitempty"`
}

// Load reads a config from path, or discovers one by walking up from dir, and
// fills in every derived default. This is what the rotating commands use.
func Load(path, dir string) (*Config, error) {
	return load(path, dir, true)
}

// LoadRaw is Load without the derived defaults, for commands that edit the file
// and write it back: a default written to disk is a decision the operator never
// made, and it stops tracking the config it was derived from.
func LoadRaw(path, dir string) (*Config, error) {
	return load(path, dir, false)
}

func load(path, dir string, defaults bool) (*Config, error) {
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

	if defaults {
		c.applyDefaults()
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Discover walks up from dir looking for a config, and reports where it stopped.
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

func (c *Config) applyDefaults() {
	for i := range c.Environments {
		e := &c.Environments[i]
		if e.GitHubEnv == "" {
			e.GitHubEnv = e.Name
		}
		if e.GCPSuffix == nil {
			// Only a missing key defaults; an explicit "" stays empty, which is
			// how production gets an unsuffixed name.
			s := "-" + e.Name
			e.GCPSuffix = &s
		}
	}
	for i := range c.Secrets {
		s := &c.Secrets[i]
		if s.Label == "" {
			s.Label = s.Name
		}
	}
}

func (c *Config) validate() error {
	if c.Project == "" {
		return fmt.Errorf("project is required")
	}
	if len(c.Environments) == 0 {
		return fmt.Errorf("at least one environment is required")
	}
	if len(c.Secrets) == 0 {
		return fmt.Errorf("at least one secret is required")
	}
	if c.GitHub == nil && c.GCP == nil {
		return fmt.Errorf("configure at least one store (github, gcp)")
	}

	seenEnv := map[string]bool{}
	for _, e := range c.Environments {
		if e.Name == "" {
			return fmt.Errorf("an environment has no name")
		}
		if seenEnv[e.Name] {
			return fmt.Errorf("duplicate environment %q", e.Name)
		}
		seenEnv[e.Name] = true
	}

	seenKey, seenName := map[string]bool{}, map[string]bool{}
	for _, s := range c.Secrets {
		switch {
		case s.Key == "":
			return fmt.Errorf("a secret has no key")
		case s.Name == "":
			return fmt.Errorf("secret %q has no name", s.Key)
		case seenKey[s.Key]:
			return fmt.Errorf("duplicate secret key %q", s.Key)
		case seenName[s.Name]:
			return fmt.Errorf("duplicate secret name %q", s.Name)
		}
		seenKey[s.Key], seenName[s.Name] = true, true

		for _, id := range s.Stores {
			if id != "github" && id != "gcp" {
				return fmt.Errorf("secret %q: unknown store %q", s.Key, id)
			}
		}
	}
	return nil
}

// Env finds an environment by name.
func (c *Config) Env(name string) (Environment, bool) {
	for _, e := range c.Environments {
		if e.Name == name {
			return e, true
		}
	}
	return Environment{}, false
}

// EnvNames lists every environment name, in declared order.
func (c *Config) EnvNames() []string {
	out := make([]string, 0, len(c.Environments))
	for _, e := range c.Environments {
		out = append(out, e.Name)
	}
	return out
}

// Secret finds a secret by key.
func (c *Config) Secret(key string) (Secret, bool) {
	for _, s := range c.Secrets {
		if s.Key == key {
			return s, true
		}
	}
	return Secret{}, false
}

// SecretKeys lists every secret key, in declared order.
func (c *Config) SecretKeys() []string {
	out := make([]string, 0, len(c.Secrets))
	for _, s := range c.Secrets {
		out = append(out, s.Key)
	}
	return out
}

// UsesStore reports whether a secret is written to the given store.
func (s Secret) UsesStore(id string) bool {
	if len(s.Stores) == 0 {
		return true
	}
	for _, want := range s.Stores {
		if want == id {
			return true
		}
	}
	return false
}

// Suffix is the environment's Secret Manager suffix.
func (e Environment) Suffix() string {
	if e.GCPSuffix == nil {
		return ""
	}
	return *e.GCPSuffix
}
