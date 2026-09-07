package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
)

// theme is the one huh theme the whole CLI uses.
func theme() *huh.Theme { return huh.ThemeCharm() }

// requireTTY refuses to start a wizard that cannot be answered. A form on a
// pipe otherwise fails deep inside bubbletea with an error naming a terminal
// mode rather than the thing the operator actually needs to do.
func requireTTY(what string) error {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	return fmt.Errorf("%s needs a terminal; run it interactively", what)
}

func run(fields ...huh.Field) error {
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(theme()).Run()
}

// --------------------------------------------------------------- project

// wizardConfig builds a whole config from nothing.
func wizardConfig(ctx context.Context, dir string) (*config.Config, error) {
	c := &config.Config{}

	project := filepath.Base(dir)
	if project == "." || project == string(filepath.Separator) {
		project = ""
	}
	stores := []string{"github", "gcp"}
	envs := "production, staging, development"

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Used in output and, for GCP, to derive secret names").
				Value(&project).
				Validate(notBlank("project name")),

			huh.NewMultiSelect[string]().
				Title("Which stores hold this project's secrets?").
				Description("space to toggle · enter to continue").
				Options(
					huh.NewOption("GitHub Actions — environment-scoped secrets, via gh", "github").Selected(true),
					huh.NewOption("Google Secret Manager — versioned secrets, via gcloud", "gcp").Selected(true),
				).
				Value(&stores).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return errors.New("pick at least one store")
					}
					return nil
				}),

			huh.NewInput().
				Title("Environments").
				Description("comma separated, in the order you rotate them").
				Value(&envs).
				Validate(func(s string) error {
					if len(splitList(s)) == 0 {
						return errors.New("at least one environment is required")
					}
					return nil
				}),
		),
	).WithTheme(theme()).Run()
	if err != nil {
		return nil, err
	}

	c.Project = strings.TrimSpace(project)
	for _, name := range splitList(envs) {
		c.Environments = append(c.Environments, config.Environment{Name: name})
	}

	if has(stores, "github") {
		repo := detectRepo(ctx)
		if err := run(huh.NewInput().
			Title("GitHub repository").
			Description("owner/name · blank lets gh resolve it from the working directory").
			Placeholder("owner/name").
			Value(&repo).
			Validate(optional(looksLikeRepo)),
		); err != nil {
			return nil, err
		}
		c.GitHub = &config.GitHub{Repo: strings.TrimSpace(repo)}
	}

	if has(stores, "gcp") {
		gcp := config.GCP{Prefix: c.Project + "-"}
		if err := run(
			huh.NewInput().
				Title("GCP project id").
				Description("the id, not the display name — they are often different").
				Value(&gcp.Project).
				Validate(notBlank("GCP project id")),
			huh.NewInput().
				Title("Secret name prefix").
				Description("prepended to every Secret Manager name, so projects can share one GCP project").
				Value(&gcp.Prefix),
		); err != nil {
			return nil, err
		}
		c.GCP = &gcp

		if err := wizardSuffixes(c); err != nil {
			return nil, err
		}
	}

	// A config with no secrets does not load, so the first one is not optional.
	for {
		sec, err := wizardSecret(c, nil)
		if err != nil {
			return nil, err
		}
		c.PutSecret(sec)

		another := false
		if err := run(huh.NewConfirm().
			Title("Add another secret?").
			Description(fmt.Sprintf("%d so far: %s", len(c.Secrets),
				strings.Join(c.SecretKeys(), ", "))).
			Value(&another),
		); err != nil {
			return nil, err
		}
		if !another {
			return c, nil
		}
	}
}

// wizardSuffixes asks how each environment is spelled in Secret Manager, which
// has no notion of environments and so puts them in the name.
func wizardSuffixes(c *config.Config) error {
	values := make([]string, len(c.Environments))
	fields := make([]huh.Field, 0, len(c.Environments))

	for i, e := range c.Environments {
		// Production conventionally carries no suffix: the name most consumers
		// reference is the unsuffixed one.
		values[i] = "-" + e.Name
		if isProd(e.Name) {
			values[i] = ""
		}
		fields = append(fields, huh.NewInput().
			Title(fmt.Sprintf("Secret Manager suffix for %q", e.Name)).
			Description(fmt.Sprintf("%s%s%s ← empty is legitimate, and conventional for production",
				c.GCP.Prefix, "<secret>", values[i])).
			Value(&values[i]))
	}

	if err := huh.NewForm(huh.NewGroup(fields...).
		Title("Secret Manager names").
		Description("Secret Manager has no environments, so the environment goes in the name"),
	).WithTheme(theme()).Run(); err != nil {
		return err
	}

	for i := range c.Environments {
		s := strings.TrimSpace(values[i])
		c.Environments[i].GCPSuffix = &s
	}
	return nil
}

// --------------------------------------------------------------- secret

// wizardSecret builds one secret. A non-nil base prefills every field, which is
// what makes `config edit` an edit rather than a retype.
//
// Four questions, because there are four things to know. Nothing here asks what
// a valid value looks like: no answer to that question stays true, and a stale
// answer refuses a correct credential.
func wizardSecret(c *config.Config, base *config.Secret) (config.Secret, error) {
	var sec config.Secret
	if base != nil {
		sec = *base
	}

	var (
		name   = sec.Name
		key    = sec.Key
		label  = sec.Label
		help   = sec.Help
		stores = secretStores(c, sec)
	)

	fields := []huh.Field{
		huh.NewInput().
			Title("Secret name").
			Description("the environment variable / GitHub secret name, e.g. WORKOS_API_KEY").
			Value(&name).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if err := notBlank("secret name")(s); err != nil {
					return err
				}
				if base == nil || base.Name != s {
					for _, other := range c.Secrets {
						if other.Name == s {
							return fmt.Errorf("%s is already configured", s)
						}
					}
				}
				// Derive the key from the name as it is typed, so the next
				// field is prefilled rather than asked cold.
				if key == "" || (base == nil && key == deriveKey(sec.Name)) {
					key = deriveKey(s)
				}
				sec.Name = s
				return nil
			}),

		huh.NewInput().
			Title("Key").
			Description("stable id, used by --only and as the Secret Manager name").
			Value(&key).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if err := notBlank("key")(s); err != nil {
					return err
				}
				if base == nil || base.Key != s {
					if c.SecretIndex(s) >= 0 {
						return fmt.Errorf("key %q is already configured", s)
					}
				}
				return nil
			}),

		huh.NewInput().
			Title("Label").
			Description("what the rotation prompt calls it · blank uses the name").
			Value(&label),

		huh.NewInput().
			Title("Where to find it").
			Description("shown at the prompt, e.g. \"Dashboard › API Keys\"").
			Value(&help),
	}

	// Only worth asking when there is more than one store to choose between.
	if storeCount(c) > 1 {
		fields = append(fields, huh.NewMultiSelect[string]().
			Title("Stores").
			Description("where this one is written · all of them is the usual answer").
			Options(storeOptions(c, stores)...).
			Value(&stores).
			Validate(func(v []string) error {
				if len(v) == 0 {
					return errors.New("a secret nothing stores cannot be rotated")
				}
				return nil
			}))
	}

	if err := huh.NewForm(huh.NewGroup(fields...).Title(secretGroupTitle(base))).
		WithTheme(theme()).Run(); err != nil {
		return config.Secret{}, err
	}

	out := config.Secret{
		Key:   strings.TrimSpace(key),
		Name:  strings.TrimSpace(name),
		Label: strings.TrimSpace(label),
		Help:  strings.TrimSpace(help),
	}
	// An empty list means every store, which is both shorter to read and
	// correct by default when a store is added later.
	if len(stores) != storeCount(c) {
		out.Stores = stores
	}
	return out, nil
}

func secretGroupTitle(base *config.Secret) string {
	if base == nil {
		return "New secret"
	}
	return "Editing " + base.Name
}

// --------------------------------------------------------------- helpers

func detectRepo(ctx context.Context) string {
	res, err := shell.Run(ctx, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
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

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func storeOptions(c *config.Config, selected []string) []huh.Option[string] {
	var opts []huh.Option[string]
	if c.GitHub != nil {
		opts = append(opts, huh.NewOption("GitHub Actions", "github").Selected(has(selected, "github")))
	}
	if c.GCP != nil {
		opts = append(opts, huh.NewOption("Google Secret Manager", "gcp").Selected(has(selected, "gcp")))
	}
	return opts
}

func storeCount(c *config.Config) int {
	n := 0
	if c.GitHub != nil {
		n++
	}
	if c.GCP != nil {
		n++
	}
	return n
}

// secretStores is the secret's store list expanded to actual store ids, because
// an empty list means "all of them" and a multiselect cannot show that.
func secretStores(c *config.Config, sec config.Secret) []string {
	if len(sec.Stores) > 0 {
		return append([]string(nil), sec.Stores...)
	}
	var out []string
	if c.GitHub != nil {
		out = append(out, "github")
	}
	if c.GCP != nil {
		out = append(out, "gcp")
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func isProd(name string) bool {
	return name == "production" || name == "prod"
}

// deriveKey turns WORKOS_API_KEY into workos-api-key.
func deriveKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
}
