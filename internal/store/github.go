package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
)

// GitHub writes environment-scoped GitHub Actions secrets via the gh CLI.
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

func (g *GitHub) Target(s config.Secret, e config.Environment) string {
	return e.GitHubEnv + "/" + s.Name
}

func (g *GitHub) List(ctx context.Context, e config.Environment) (map[string]bool, error) {
	res, err := shell.Run(ctx, "gh", "secret", "list",
		"--repo", g.repo, "--env", e.GitHubEnv, "--json", "name")
	if err != nil {
		// An environment with no secrets, or one that does not exist yet, is
		// not an error here: the caller only wants to label a write "create"
		// rather than "overwrite".
		return map[string]bool{}, nil
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &rows); err != nil {
		return nil, fmt.Errorf("parsing gh secret list: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.Name] = true
	}
	return out, nil
}

func (g *GitHub) Write(ctx context.Context, s config.Secret, e config.Environment, value string) error {
	// stdin, not --body: --body would put the credential on the command line,
	// where it is visible in `ps` to every other user on the machine.
	_, err := shell.RunStdin(ctx, value, "gh", "secret", "set", s.Name,
		"--repo", g.repo, "--env", e.GitHubEnv)
	return err
}

func (g *GitHub) Read(ctx context.Context, s config.Secret, e config.Environment) (string, error) {
	return "", ErrWriteOnly
}

// EnsureEnvironments creates the GitHub Environments the secrets are scoped to.
//
// An environment-scoped secret cannot be written until its environment exists,
// and the PUT is idempotent, so this runs on every rotation rather than only
// under `init`: one redundant API call is cheaper than a rotation that fails
// because someone deleted an environment last week.
func (g *GitHub) EnsureEnvironments(ctx context.Context, envs []config.Environment, dryRun bool, log func(string, bool)) error {
	for _, e := range envs {
		path := fmt.Sprintf("repos/%s/environments/%s", g.repo, e.GitHubEnv)
		if shell.Quiet(ctx, "gh", "api", path) {
			log(fmt.Sprintf("environment %s exists", e.GitHubEnv), true)
			continue
		}
		if dryRun {
			log(fmt.Sprintf("environment %s is missing (dry run - not creating it)", e.GitHubEnv), false)
			continue
		}
		if _, err := shell.Run(ctx, "gh", "api", "--method", "PUT", path); err != nil {
			return fmt.Errorf("could not create environment %s: %w\n"+
				"  environments on a private repository need GitHub Pro / Team / Enterprise,\n"+
				"  and the token needs the 'repo' scope", e.GitHubEnv, err)
		}
		log(fmt.Sprintf("created environment %s", e.GitHubEnv), true)
	}
	return nil
}
