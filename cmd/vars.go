package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

// GitHub Actions variables are the non-secret half of a workflow's environment:
// a region, a project id, a service account address. They live only in GitHub —
// Secret Manager has no notion of a value that is not a secret — so everything
// here is GitHub-only by construction, and says so when there is no github
// block to work with.
//
// They are handled apart from secrets on purpose. Their values are public, so
// they are printed in full, read back after a write, and never fingerprinted.
// Sharing the secret path would mean either hiding a public value for no reason
// or teaching that path to sometimes print what it is given.

func newVarCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "var",
		Aliases: []string{"vars", "variable", "variables"},
		Short:   "List, set, and remove GitHub Actions variables",
		Long: `GitHub Actions variables: the non-secret values a workflow reads.

A variable is not a secret. Anyone who can read the repository can read its
value, GitHub prints it back on request, and so does this command. Put a region
or a project id here; put anything that would matter if it leaked in a secret.

Variables exist only in GitHub. Secret Manager stores secrets, and a non-secret
there would be a secret with a plaintext value, so nothing in this command
touches GCP.

Like secrets, they are repository-wide or scoped to one environment, and an
environment has to exist before a variable can be put in it — see ` + "`secretman env add`" + `.`,
		Example: `  secretman var list
  secretman var set                       # pick name, scopes, and value
  secretman var set GCP_REGION us-east1
  secretman var rm`,
	}
	c.AddCommand(newVarListCmd(), newVarSetCmd(), newVarRmCmd("rm"))
	return c
}

// varSession is preflight plus "which environments exist", which is the only
// thing these commands need beyond the store itself.
func varSession(ctx context.Context, what string) (*session, []string, error) {
	s, err := loadSession()
	if err != nil {
		return nil, nil, err
	}
	ui.Title("secretman › var %s", what)
	if s.gh == nil {
		return nil, nil, errors.New("no github block in the config; variables are a GitHub concept")
	}
	if err := s.gh.Preflight(ctx); err != nil {
		return nil, nil, err
	}
	envs, _ := s.gh.Environments(ctx)
	return s, envs, nil
}

// --------------------------------------------------------------- list

func newVarListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List every variable, with its value",
		Long: `Lists what the repository holds, repository-wide first, then per environment.

Values are printed. They are public — this is the difference between a variable
and a secret, and hiding them here would only make it harder to see that
something belongs on the other side of that line.`,
		Example: `  secretman var list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, _, err := varSession(ctx, "list")
			if err != nil {
				return err
			}
			vars, err := s.gh.Variables(ctx)
			if err != nil {
				return err
			}
			ui.Step("GitHub Actions variables")
			if len(vars) == 0 {
				ui.Note("nothing")
				ui.Blank()
				return nil
			}
			rows := make([][]string, 0, len(vars))
			for _, v := range vars {
				rows = append(rows, []string{v.Name, v.Scope(), v.Value})
			}
			ui.Blank()
			fmt.Println(ui.Indent(ui.Table([]string{"NAME", "SCOPE", "VALUE"}, rows), 2))
			ui.Blank()
			ui.Note("values are public — anything that must not be is a secret, not a variable")
			ui.Blank()
			return nil
		},
	}
}

// --------------------------------------------------------------- set

func newVarSetCmd() *cobra.Command {
	var envs []string
	var repoWide bool

	c := &cobra.Command{
		Use:     "set [NAME] [VALUE]",
		Aliases: []string{"add"},
		Short:   "Create or change a variable",
		Long: `Writes a value to one or more scopes. Creating and changing are the same call.

With no arguments it asks: the name, where it belongs — repository-wide, or any
combination of environments — and the value. An existing value is offered as the
starting point, so changing one is an edit rather than a retype.

With a name and a value it writes them straight, to the scopes named by --env,
or repository-wide when none are. That form needs no terminal and is the one to
put in a script.

Every write is read back and compared, because unlike a secret a variable can
be.`,
		Example: `  secretman var set
  secretman var set GCP_REGION us-east1
  secretman var set GCP_REGION us-east1 --env staging --env production`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			name, value := "", ""
			if len(args) > 0 {
				name = strings.TrimSpace(args[0])
			}
			if len(args) > 1 {
				value = args[1]
			}

			// Two arguments means the operator has said everything there is to
			// ask, so nothing is asked and no terminal is needed.
			scripted := len(args) == 2

			s, existing, err := varSession(ctx, "set")
			if err != nil {
				return err
			}
			if !scripted {
				if err := requireTTY("var set"); err != nil {
					return err
				}
			}

			var targets []store.Variable

			if scripted {
				clean, err := cleanVarValue(value)
				if err != nil {
					return err
				}
				scopes := envs
				if repoWide || len(scopes) == 0 {
					scopes = append([]string{""}, scopes...)
				}
				for _, env := range scopes {
					if env != "" && !has(existing, env) {
						return fmt.Errorf("no GitHub Environment named %q; "+
							"`secretman env add %s` creates it", env, env)
					}
					targets = append(targets,
						store.Variable{Name: name, Value: clean, Env: env})
				}
			} else {
				targets, err = askVariable(ctx, s, name, existing)
				if err != nil {
					return err
				}
				if len(targets) == 0 {
					ui.Blank()
					ui.Note("Nothing selected.")
					ui.Blank()
					return nil
				}
			}

			return s.writeVariables(ctx, targets)
		},
	}
	c.Flags().StringSliceVarP(&envs, "env", "e", nil,
		"environment to write to; repeatable (default: repository-wide)")
	c.Flags().BoolVar(&repoWide, "repo-wide", false,
		"also write the repository-wide variable when --env is given")
	return c
}

// askVariable is the interactive half of set: name, scopes, value.
func askVariable(ctx context.Context, s *session, name string, envs []string) ([]store.Variable, error) {
	current, err := s.gh.Variables(ctx)
	if err != nil {
		return nil, err
	}

	if name == "" {
		// Existing names are offered first, because changing a variable is the
		// common case and retyping a name is how a second, near-identical one
		// gets created by accident.
		seen := map[string]bool{}
		opts := []huh.Option[string]{huh.NewOption("(a new variable)", "")}
		for _, v := range current {
			if !seen[v.Name] {
				seen[v.Name] = true
				opts = append(opts, huh.NewOption(v.Name, v.Name))
			}
		}
		if len(opts) > 1 {
			if err := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which variable?").
					Options(opts...).
					Value(&name),
			)).WithTheme(theme()).Run(); err != nil {
				return nil, err
			}
		}
	}

	if name == "" {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Variable name").
				Description("as an environment variable, e.g. GCP_REGION").
				Value(&name).
				Validate(notBlank("variable name")),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
	}

	// What each scope holds now, so the picker can show it and the value prompt
	// can start from it.
	held := map[string]string{}
	for _, v := range current {
		if v.Name == name {
			held[v.Env] = v.Value
		}
	}

	label := func(env, shown string) string {
		if v, ok := held[env]; ok {
			return fmt.Sprintf("%s — currently %q", shown, v)
		}
		return shown + " — not set"
	}

	opts := []huh.Option[string]{
		huh.NewOption(label("", "repository-wide (every workflow)"), ""),
	}
	for _, env := range envs {
		opts = append(opts, huh.NewOption(label(env, "environment: "+env), env))
	}

	var chosen []string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Where does " + name + " belong?").
			Description("space to toggle · enter to continue · `secretman env add` creates an environment").
			Options(opts...).
			Value(&chosen),
	)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		return nil, nil
	}

	// The existing value is the starting point when every chosen scope agrees
	// on one; where they disagree there is no sensible prefill, so there is none.
	value := ""
	for i, env := range chosen {
		if i == 0 {
			value = held[env]
			continue
		}
		if held[env] != value {
			value = ""
			break
		}
	}

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Value for " + name).
			Description("public — visible to anyone who can read the repository").
			Value(&value).
			Validate(func(in string) error {
				_, err := cleanVarValue(in)
				return err
			}),
	)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}
	clean, err := cleanVarValue(value)
	if err != nil {
		return nil, err
	}

	out := make([]store.Variable, 0, len(chosen))
	for _, env := range chosen {
		out = append(out, store.Variable{Name: name, Value: clean, Env: env})
	}
	return out, nil
}

// cleanVarValue trims a pasted value and refuses what GitHub cannot hold on one
// line. It is deliberately lighter than secretval.Sanitize: a variable is public
// and is frequently a URL or a quoted-looking string, so nothing is stripped
// beyond surrounding whitespace, and the value can be shown in the error.
func cleanVarValue(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New("value is required")
	}
	for _, r := range v {
		if !unicode.IsPrint(r) {
			return "", errors.New("contains a newline, tab, or control character")
		}
	}
	return v, nil
}

// writeVariables confirms, writes, and reads back.
func (s *session) writeVariables(ctx context.Context, targets []store.Variable) error {
	rows := make([][]string, 0, len(targets))
	for _, v := range targets {
		was := "not set"
		rows = append(rows, []string{v.Name, v.Scope(), v.Value, was})
	}
	// What is there now, so the table can say what changes rather than only
	// what will be written.
	if current, err := s.gh.Variables(ctx); err == nil {
		for i, v := range targets {
			for _, c := range current {
				if c.ID() == v.ID() {
					rows[i][3] = c.Value
				}
			}
		}
	}

	ui.Step("About to write")
	fmt.Println(ui.Indent(ui.Table(
		[]string{"NAME", "SCOPE", "NEW VALUE", "CURRENT"}, rows), 2))

	if flagDryRun {
		ui.Blank()
		ui.OK("dry run complete; nothing written")
		ui.Blank()
		return nil
	}

	ui.Blank()
	proceed := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Write %d variable(s)?", len(targets))).
			Affirmative("Write them").
			Negative("Abort").
			Value(&proceed),
	)).WithTheme(theme()).Run(); err != nil {
		return err
	}
	if !proceed {
		return errors.New("aborted; nothing written")
	}

	ui.Step("Writing")
	failed := 0
	for _, v := range targets {
		if err := s.gh.SetVariable(ctx, v); err != nil {
			ui.Warn("%s: FAILED — %v", v.Label(), err)
			failed++
			continue
		}
		ui.OK("%s = %s", v.Label(), v.Value)
	}

	// A variable can be read back, so it is: this is the check a secret can
	// never have, and it costs one call.
	if after, err := s.gh.Variables(ctx); err == nil {
		for _, v := range targets {
			for _, c := range after {
				if c.ID() == v.ID() && c.Value != v.Value {
					ui.Warn("%s — READ-BACK MISMATCH: wrote %q, read %q",
						v.Label(), v.Value, c.Value)
					failed++
				}
			}
		}
	}

	ui.Blank()
	if failed > 0 {
		return fmt.Errorf("%d variable write(s) failed", failed)
	}
	ui.Note("a job only sees an environment's variables if it declares `environment: <name>`")
	ui.Blank()
	return nil
}

// --------------------------------------------------------------- rm

func newVarRmCmd(use string) *cobra.Command {
	var force bool
	var envs []string

	c := &cobra.Command{
		Use:     use + " [NAME...]",
		Aliases: aliasesFor(use),
		Short:   "Remove variables",
		Long: `Deletes variables from the repository.

With no arguments it lists everything and deletes what you tick off. With names
it deletes those, from the scopes named by --env, or repository-wide when none
are given.

Unlike a deleted secret, a deleted variable is not a lost credential: the value
was public and is printed before it goes, so it can be put back. What does break
is any workflow reading it, at its next run.`,
		Example: `  secretman var rm
  secretman var rm GCP_REGION
  secretman var rm GCP_REGION --env staging --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, existing, err := varSession(ctx, "rm")
			if err != nil {
				return err
			}
			current, err := s.gh.Variables(ctx)
			if err != nil {
				return err
			}
			if len(current) == 0 {
				ui.Blank()
				ui.Note("No variables in this repository.")
				ui.Blank()
				return nil
			}

			var chosen []store.Variable
			if len(args) == 0 {
				if err := requireTTY("var rm"); err != nil {
					return err
				}
				chosen, err = pickVariables(current)
				if err != nil {
					return err
				}
			} else {
				scopes := envs
				if len(scopes) == 0 {
					scopes = []string{""}
				}
				for _, name := range args {
					for _, env := range scopes {
						if env != "" && !has(existing, env) {
							return fmt.Errorf("no GitHub Environment named %q", env)
						}
						want := store.Variable{Name: name, Env: env}
						found := false
						for _, c := range current {
							if c.ID() == want.ID() {
								chosen = append(chosen, c)
								found = true
							}
						}
						if !found {
							return fmt.Errorf("no variable %s", want.Label())
						}
					}
				}
			}

			if len(chosen) == 0 {
				ui.Blank()
				ui.Note("Nothing selected.")
				ui.Blank()
				return nil
			}

			ui.Step("About to remove")
			for _, v := range chosen {
				ui.Warn("%s = %s", v.Label(), v.Value)
			}
			ui.Blank()
			ui.Note("the values are printed above; a variable is public, so this is recoverable")
			ui.Blank()

			if flagDryRun {
				ui.OK("dry run complete; nothing removed")
				ui.Blank()
				return nil
			}

			if !force {
				confirmed := false
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Remove %d variable(s)?", len(chosen))).
						Description("workflows reading them fail at their next run").
						Affirmative("Yes, remove them").
						Negative("No, keep them").
						Value(&confirmed),
				)).WithTheme(theme()).Run(); err != nil {
					return err
				}
				if !confirmed {
					ui.Blank()
					ui.Note("Nothing removed.")
					ui.Blank()
					return nil
				}
			}

			ui.Step("Removing")
			failed := 0
			for _, v := range chosen {
				if err := s.gh.DeleteVariable(ctx, v); err != nil {
					ui.Warn("%s: FAILED — %v", v.Label(), err)
					failed++
					continue
				}
				ui.OK("%s — removed", v.Label())
			}
			ui.Blank()
			if failed > 0 {
				return fmt.Errorf("%d removal(s) failed", failed)
			}
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false,
		"skip the confirmation (the list is still shown)")
	c.Flags().StringSliceVarP(&envs, "env", "e", nil,
		"environment to remove from; repeatable (default: repository-wide)")
	return c
}

// pickVariables is the checkbox list rm uses, with the value on every line so
// nothing is deleted by name alone.
func pickVariables(vars []store.Variable) ([]store.Variable, error) {
	opts := make([]huh.Option[string], 0, len(vars))
	index := map[string]store.Variable{}
	for _, v := range vars {
		opts = append(opts, huh.NewOption(
			fmt.Sprintf("%s [%s] = %s", v.Name, v.Scope(), v.Value), v.ID()))
		index[v.ID()] = v
	}

	var ids []string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Remove which variables?").
			Description("space to toggle · enter to continue").
			Options(opts...).
			Value(&ids),
	)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}

	var out []store.Variable
	for _, v := range vars {
		for _, id := range ids {
			if id == v.ID() {
				out = append(out, index[id])
			}
		}
	}
	return out, nil
}
