package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
)

// GitHub reads and writes GitHub Actions secrets via the gh CLI.
//
// Names are listable; values are not. That asymmetry is why this store can be
// enumerated and walked like any other, but can never be verified: the digest
// of what was sent is the only record a write here will ever leave.
type GitHub struct {
	repo string
}

// NewGitHub builds the store. repo may be empty, in which case Preflight asks
// gh which repository the working directory belongs to.
func NewGitHub(cfg *config.Config) *GitHub {
	g := &GitHub{}
	if cfg.GitHub != nil {
		g.repo = cfg.GitHub.Repo
	}
	return g
}

func (g *GitHub) ID() string     { return "github" }
func (g *GitHub) Label() string  { return "GitHub Actions" }
func (g *GitHub) Readable() bool { return false }

// Repo is the resolved owner/name, available after Preflight.
func (g *GitHub) Repo() string { return g.repo }

func (g *GitHub) Preflight(ctx context.Context) error {
	if err := shell.Require("gh"); err != nil {
		return err
	}
	if _, err := shell.Run(ctx, "gh", "auth", "status"); err != nil {
		return fmt.Errorf("gh is not authenticated; run: gh auth login")
	}
	if g.repo == "" {
		res, err := shell.Run(ctx, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
		if err != nil {
			return fmt.Errorf("not inside a GitHub repository that gh can resolve; " +
				"set github.repo in the config")
		}
		g.repo = strings.TrimSpace(res.Stdout)
	}
	if !shell.Quiet(ctx, "gh", "repo", "view", g.repo, "--json", "name") {
		return fmt.Errorf("cannot reach repository %s", g.repo)
	}
	return nil
}

// Environments lists the repository's GitHub Environments.
func (g *GitHub) Environments(ctx context.Context) ([]string, error) {
	res, err := shell.Run(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/environments", g.repo), "-q", ".environments[].name")
	if err != nil {
		// A repository with no environments returns an error on some plans
		// rather than an empty list, and that is not a failure: it means every
		// secret here is repository-scoped.
		return nil, nil
	}
	var out []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Enumerate lists repository-scoped secrets, then each environment's.
func (g *GitHub) Enumerate(ctx context.Context) ([]Entry, error) {
	var out []Entry

	for _, name := range g.names(ctx, "") {
		out = append(out, Entry{Store: g.ID(), Name: name})
	}

	envs, err := g.Environments(ctx)
	if err != nil {
		return nil, err
	}
	for _, env := range envs {
		for _, name := range g.names(ctx, env) {
			out = append(out, Entry{Store: g.ID(), Name: name, Env: env})
		}
	}
	return out, nil
}

// names lists secret names at the repository level, or within one environment.
func (g *GitHub) names(ctx context.Context, env string) []string {
	args := []string{"secret", "list", "--repo", g.repo, "--json", "name"}
	if env != "" {
		args = append(args, "--env", env)
	}
	res, err := shell.Run(ctx, "gh", args...)
	if err != nil {
		return nil // no secrets, or no access to that environment
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(res.Stdout), &rows) != nil {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

func (g *GitHub) Write(ctx context.Context, e Entry, value string) error {
	// stdin, not --body: --body would put the credential on the command line,
	// where it is visible in `ps` to every other user on the machine.
	args := []string{"secret", "set", e.Name, "--repo", g.repo}
	if e.Env != "" {
		args = append(args, "--env", e.Env)
	}
	_, err := shell.RunStdin(ctx, value, "gh", args...)
	return err
}

func (g *GitHub) Read(ctx context.Context, e Entry) (string, error) {
	return "", ErrWriteOnly
}

// Variable is one GitHub Actions variable, as GitHub holds it.
//
// Variables are not secrets and are deliberately not Entry: their values are
// public, readable by anyone who can read the repository, and printed here in
// full. Nothing about them is fingerprinted or hidden, because pretending a
// public value is a secret is how a real secret ends up stored as one.
//
// They are also a GitHub-only concept. Secret Manager has no equivalent — a
// non-secret there would just be a secret with a plaintext value — so this
// lives on the GitHub store rather than in the Store interface.
type Variable struct {
	Name  string
	Value string
	Env   string // "" when the variable is repository-wide
}

// Label is what the operator sees: store, name, and environment when there is one.
func (v Variable) Label() string {
	if v.Env == "" {
		return fmt.Sprintf("github:%s", v.Name)
	}
	return fmt.Sprintf("github:%s:%s", v.Name, v.Env)
}

// ID is a stable key for a variable, used to track what a session has touched.
func (v Variable) ID() string { return v.Env + "\x00" + v.Name }

// Scope names where the variable lives, for tables.
func (v Variable) Scope() string {
	if v.Env == "" {
		return "(repo-wide)"
	}
	return v.Env
}

// Variables lists repository-wide variables, then each environment's.
func (g *GitHub) Variables(ctx context.Context) ([]Variable, error) {
	out := g.variables(ctx, "")

	envs, err := g.Environments(ctx)
	if err != nil {
		return nil, err
	}
	for _, env := range envs {
		out = append(out, g.variables(ctx, env)...)
	}
	return out, nil
}

// variables lists variables at the repository level, or within one environment.
func (g *GitHub) variables(ctx context.Context, env string) []Variable {
	args := []string{"variable", "list", "--repo", g.repo, "--json", "name,value"}
	if env != "" {
		args = append(args, "--env", env)
	}
	res, err := shell.Run(ctx, "gh", args...)
	if err != nil {
		return nil // no variables, or no access to that environment
	}
	var rows []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if json.Unmarshal([]byte(res.Stdout), &rows) != nil {
		return nil
	}
	out := make([]Variable, 0, len(rows))
	for _, r := range rows {
		out = append(out, Variable{Name: r.Name, Value: r.Value, Env: env})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SetVariable creates or updates a variable. gh treats set as an upsert, so
// adding and changing one are the same call.
func (g *GitHub) SetVariable(ctx context.Context, v Variable) error {
	// stdin rather than --body, for the same reason the secret path uses it:
	// argv is visible in `ps`. A variable is public, but keeping one path for
	// both means there is no second habit to get wrong.
	args := []string{"variable", "set", v.Name, "--repo", g.repo}
	if v.Env != "" {
		args = append(args, "--env", v.Env)
	}
	_, err := shell.RunStdin(ctx, v.Value, "gh", args...)
	return err
}

// DeleteVariable removes a variable.
func (g *GitHub) DeleteVariable(ctx context.Context, v Variable) error {
	args := []string{"variable", "delete", v.Name, "--repo", g.repo}
	if v.Env != "" {
		args = append(args, "--env", v.Env)
	}
	_, err := shell.Run(ctx, "gh", args...)
	return err
}

// EnsureEnvironment creates a GitHub Environment. The PUT is idempotent.
func (g *GitHub) EnsureEnvironment(ctx context.Context, name string) (created bool, err error) {
	path := fmt.Sprintf("repos/%s/environments/%s", g.repo, name)
	if shell.Quiet(ctx, "gh", "api", path) {
		return false, nil
	}
	if _, err := shell.Run(ctx, "gh", "api", "--method", "PUT", path); err != nil {
		return false, fmt.Errorf("could not create environment %s: %w\n"+
			"  environments on a private repository need GitHub Pro / Team / Enterprise,\n"+
			"  and the token needs the 'repo' scope", name, err)
	}
	return true, nil
}
