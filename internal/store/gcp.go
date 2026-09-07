package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
)

// GCP writes Google Secret Manager versions via the gcloud CLI.
//
// This is the only readable store, which makes it the only place a
// cross-environment transposition is detectable and the only place a write can
// be verified by reading it back.
type GCP struct {
	project string
	// prefix is prepended to every name, so several projects can share one GCP
	// project. Decided once, at init; never per secret.
	prefix string
	// runtimeSA, when set, is granted secretAccessor on every secret this tool
	// creates. A new secret the runtime cannot read fails at the next cold
	// start, a long way from here.
	runtimeSA string
}

// NewGCP builds the store.
func NewGCP(cfg *config.Config, projectOverride string) *GCP {
	g := &GCP{}
	if cfg.GCP != nil {
		g.project = cfg.GCP.Project
		g.prefix = cfg.GCP.Prefix
	}
	if projectOverride != "" {
		g.project = projectOverride
	}
	return g
}

func (g *GCP) ID() string     { return "gcp" }
func (g *GCP) Label() string  { return "Google Secret Manager" }
func (g *GCP) Readable() bool { return true }

// Project is the resolved project ID.
func (g *GCP) Project() string { return g.project }

// SetRuntimeServiceAccount sets the account granted read access to new secrets.
func (g *GCP) SetRuntimeServiceAccount(sa string) { g.runtimeSA = strings.TrimSpace(sa) }

// RuntimeServiceAccount returns the configured account, if any.
func (g *GCP) RuntimeServiceAccount() string { return g.runtimeSA }

func (g *GCP) Preflight(ctx context.Context) error {
	if err := shell.Require("gcloud"); err != nil {
		return err
	}
	if g.project == "" {
		return fmt.Errorf("no GCP project configured; set gcp.project or pass --gcp-project")
	}

	res, err := shell.Run(ctx, "gcloud", "auth", "list",
		"--filter=status:ACTIVE", "--format=value(account)")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return fmt.Errorf("gcloud has no active account; run: gcloud auth login")
	}

	if !shell.Quiet(ctx, "gcloud", "projects", "describe", g.project) {
		return fmt.Errorf("cannot reach project %q\n"+
			"  the GCP project ID is not always the project NAME. Check with:\n"+
			"    gcloud projects list\n"+
			"  then pass --gcp-project <id>, or fix gcp.project in the config.\n"+
			"  If nothing is listed, the active account may be the wrong one:\n"+
			"    gcloud auth login", g.project)
	}

	// A fresh project has Secret Manager switched off. Checked here rather than
	// discovered at the first write, because a rotation that fails halfway
	// leaves the stores disagreeing about which credential is current.
	res, err = shell.Run(ctx, "gcloud", "services", "list", "--enabled",
		"--project="+g.project,
		"--filter=config.name:secretmanager.googleapis.com",
		"--format=value(config.name)")
	if err != nil || strings.TrimSpace(res.Stdout) == "" {
		return fmt.Errorf("the Secret Manager API is not enabled on %s. Enable it once:\n"+
			"    gcloud services enable secretmanager.googleapis.com --project=%s\n"+
			"  or run with --store github to skip Google Secret Manager entirely",
			g.project, g.project)
	}
	return nil
}

func (g *GCP) Target(s config.Secret, e config.Environment) string {
	return g.prefix + s.Key + e.Suffix()
}

func (g *GCP) List(ctx context.Context, e config.Environment) (map[string]bool, error) {
	res, err := shell.Run(ctx, "gcloud", "secrets", "list",
		"--project="+g.project, "--format=json(name)")
	if err != nil {
		return map[string]bool{}, nil
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &rows); err != nil {
		return nil, fmt.Errorf("parsing gcloud secrets list: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		// The API returns projects/<n>/secrets/<id>; only the id is useful.
		out[r.Name[strings.LastIndexByte(r.Name, '/')+1:]] = true
	}
	return out, nil
}

func (g *GCP) exists(ctx context.Context, name string) bool {
	return shell.Quiet(ctx, "gcloud", "secrets", "describe", name, "--project="+g.project)
}

func (g *GCP) Write(ctx context.Context, s config.Secret, e config.Environment, value string) error {
	name := g.Target(s, e)

	if !g.exists(ctx, name) {
		if _, err := shell.Run(ctx, "gcloud", "secrets", "create", name,
			"--project="+g.project, "--replication-policy=automatic"); err != nil {
			return fmt.Errorf("creating %s: %w", name, err)
		}
	}

	// --data-file=- reads stdin, so the value never becomes an argument, and
	// shell.RunStdin adds no trailing newline.
	if _, err := shell.RunStdin(ctx, value, "gcloud", "secrets", "versions", "add", name,
		"--project="+g.project, "--data-file=-"); err != nil {
		return fmt.Errorf("adding a version to %s: %w", name, err)
	}

	if g.runtimeSA != "" {
		if _, err := shell.Run(ctx, "gcloud", "secrets", "add-iam-policy-binding", name,
			"--project="+g.project,
			"--role=roles/secretmanager.secretAccessor",
			"--member=serviceAccount:"+g.runtimeSA); err != nil {
			// Not fatal: the value is written and correct. An IAM binding that
			// already exists, or a caller without setIamPolicy, should not fail
			// a rotation that otherwise succeeded.
			return fmt.Errorf("wrote %s, but could not grant %s access: %w",
				name, g.runtimeSA, err)
		}
	}
	return nil
}

func (g *GCP) Read(ctx context.Context, s config.Secret, e config.Environment) (string, error) {
	name := g.Target(s, e)
	if !g.exists(ctx, name) {
		return "", ErrNotFound
	}
	res, err := shell.Run(ctx, "gcloud", "secrets", "versions", "access", "latest",
		"--secret="+name, "--project="+g.project)
	if err != nil {
		return "", err
	}
	// No trimming: the stored payload is returned verbatim, because a stray
	// newline that got in some other way is exactly what a verify should catch.
	return res.Stdout, nil
}

// Versions lists the enabled version numbers of a secret, newest first.
func (g *GCP) Versions(ctx context.Context, s config.Secret, e config.Environment) ([]string, error) {
	name := g.Target(s, e)
	res, err := shell.Run(ctx, "gcloud", "secrets", "versions", "list", name,
		"--project="+g.project, "--filter=state:ENABLED", "--format=value(name)")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
