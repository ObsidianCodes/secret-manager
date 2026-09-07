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
var DefaultFilenames = []string{"secrets.yaml", "secrets.yml", ".secrets.yaml"}

// Config is a whole project's secret specification.
type Config struct {
	Project      string        `yaml:"project"`
	GitHub       *GitHub       `yaml:"github"`
	GCP          *GCP          `yaml:"gcp"`
	Environments []Environment `yaml:"environments"`
	Secrets      []Secret      `yaml:"secrets"`

	// Path is where this config was loaded from. Not serialised.
	Path string `yaml:"-"`
}

// GitHub configures the GitHub Actions store.
type GitHub struct {
	// Repo is owner/name. Empty means "ask gh which repo we are in".
	Repo string `yaml:"repo"`
}

// GCP configures the Google Secret Manager store.
type GCP struct {
	// Project is the project ID, which is not always the project NAME.
	Project string `yaml:"project"`
	// Prefix is prepended to every secret name, so several projects can share
	// one GCP project without colliding.
	Prefix string `yaml:"prefix"`
}

// Environment is one deployment target.
type Environment struct {
	Name string `yaml:"name"`
	// GitHubEnv is the GitHub Environment name. Defaults to Name.
	GitHubEnv string `yaml:"githubEnv"`
	// GCPSuffix distinguishes this environment's Secret Manager names.
	// Secret Manager has no concept of environments, so it goes in the name.
	// The empty string is legitimate, and conventionally means production.
	GCPSuffix *string `yaml:"gcpSuffix"`
}

// Kind selects the validation rules for a secret.
type Kind string

const (
	// KindOpaque accepts any single-line value.
	KindOpaque Kind = "opaque"
	// KindPrefixed requires a known prefix; used for provider-issued keys.
	KindPrefixed Kind = "prefixed"
	// KindPassword enforces a minimum length and can be generated.
	KindPassword Kind = "password"
	// KindURL requires an absolute URL, https outside the allowed environments.
	KindURL Kind = "url"
)

// Secret is one credential, in every environment.
type Secret struct {
	// Key is the stable identifier used by --only and in output.
	Key string `yaml:"key"`
	// Name is the environment variable / GitHub secret name.
	Name string `yaml:"name"`
	// Label is what the prompt says.
	Label string `yaml:"label"`
	// Help is the dimmed line under the prompt.
	Help string `yaml:"help"`

	Kind Kind `yaml:"kind"`

	// Prefix is required for KindPrefixed.
	Prefix string `yaml:"prefix"`
	// Conflicts maps a prefix that is NOT this secret to what it actually is,
	// so a transposed paste is named rather than merely rejected. Two adjacent
	// fields in a dashboard get swapped constantly, and the swap otherwise
	// fails at the first login rather than at the prompt.
	Conflicts map[string]string `yaml:"conflicts"`

	// MinLength is the shortest acceptable value. 0 means the kind's default.
	MinLength int `yaml:"minLength"`

	// Generate offers "generate a random value" at the prompt.
	Generate bool `yaml:"generate"`
	// GenerateBytes is how many random bytes to draw. 0 means 32.
	GenerateBytes int `yaml:"generateBytes"`

	// AllowInsecureIn lists environments where http:// and localhost are
	// acceptable for a KindURL secret.
	AllowInsecureIn []string `yaml:"allowInsecureIn"`

	// EnvMarkers map a value prefix to the ONE environment it belongs to.
	// This is what catches a live key pasted into staging without any network
	// call, and it works on a first-time setup where nothing is stored yet.
	EnvMarkers map[string]string `yaml:"envMarkers"`

	// GCPName overrides the derived Secret Manager base name.
	GCPName string `yaml:"gcpName"`
	// Stores restricts this secret to some stores by ID ("github", "gcp").
	// Empty means every configured store.
	Stores []string `yaml:"stores"`
}

// Load reads a config from path, or discovers one by walking up from dir.
func Load(path, dir string) (*Config, error) {
	if path == "" {
		found, err := discover(dir)
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

	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func discover(dir string) (string, error) {
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
			return "", fmt.Errorf("no %s found here or in any parent directory\n"+
				"  create one, or pass --config <path>", DefaultFilenames[0])
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
		if s.Kind == "" {
			s.Kind = KindOpaque
		}
		if s.Label == "" {
			s.Label = s.Name
		}
		if s.GCPName == "" {
			s.GCPName = c.gcpPrefix() + s.Key
		}
		if s.GenerateBytes == 0 {
			s.GenerateBytes = 32
		}
		if s.Kind == KindPassword && s.MinLength == 0 {
			s.MinLength = 32
		}
	}
}

func (c *Config) gcpPrefix() string {
	if c.GCP == nil {
		return ""
	}
	return c.GCP.Prefix
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

		switch s.Kind {
		case KindOpaque, KindPassword, KindURL:
		case KindPrefixed:
			if s.Prefix == "" {
				return fmt.Errorf("secret %q is kind 'prefixed' but sets no prefix", s.Key)
			}
		default:
			return fmt.Errorf("secret %q has unknown kind %q", s.Key, s.Kind)
		}

		for prefix, env := range s.EnvMarkers {
			if !seenEnv[env] {
				return fmt.Errorf("secret %q: envMarkers[%q] names unknown environment %q",
					s.Key, prefix, env)
			}
		}
		for _, env := range s.AllowInsecureIn {
			if !seenEnv[env] {
				return fmt.Errorf("secret %q: allowInsecureIn names unknown environment %q",
					s.Key, env)
			}
		}
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
