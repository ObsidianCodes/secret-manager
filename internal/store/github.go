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
