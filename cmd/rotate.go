package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/secretval"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

var flagOnly []string

func newRotateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rotate <environment>",
		Short: "Rotate one environment's secrets in every store",
		Long: `Prompts for each secret and writes it to every store configured for it.

Each prompt is preceded by the exact names it is about to overwrite — the store,
the environment, and the name within it — so the thing being changed is on
screen before anything is typed. Every prompt also offers a value generated from
crypto/rand, for the secrets you invent rather than collect.

Leave a prompt blank to leave that secret alone. Nothing is written until the
confirmation at the end, so interrupting before it costs nothing.

Every value is handled the same way, whatever provider issued it:

  * pasted NAME=, surrounding quotes and stray whitespace are stripped, and the
    stripping is reported, because the value written is then not the value typed
  * a value carrying a control character is refused outright: that is almost
    always a terminal-wrapped paste, and repairing it by guessing would write a
    silently truncated credential
  * a value identical to one another environment already holds is refused

The last is the one worth the trouble, and the only one that needs anything
fetched. A credential pasted from the wrong environment is accepted by every
store, passes every syntactic check, and only fails later, in production, as an
authentication error that names nothing useful.`,
		Example: `  # Pick from a list, then prompt for each
  secretman rotate staging

  # One secret, no picker — the emergency path
  secretman rotate production --only workos-api-key

  # Everything except the writes
  secretman rotate production --dry-run

  # A value another environment already holds, on purpose
  secretman rotate development --only workos-redirect-uri --no-cross-check`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRotate(cmd.Context(), args[0])
		},
	}
	c.Flags().StringSliceVar(&flagOnly, "only", nil, "rotate only these secret keys (skips the picker)")
	return c
}

// pending is one secret the operator has supplied a value for.
type pending struct {
	sec   config.Secret
	value string
	// generated records that the value came from crypto/rand rather than a
	// paste, which changes the advice printed at the end.
	generated bool
	notes     []string
}

func runRotate(ctx context.Context, envName string) error {
	s, err := loadSession()
	if err != nil {
		return err
	}
	env, err := s.resolveEnv(envName)
	if err != nil {
		return err
	}

	ui.Title("%s › %s", s.cfg.Project, env.Name)

	if err := s.preflight(ctx); err != nil {
		return err
	}

	// Environments must exist before an environment-scoped secret can be
	// written to one.
	for _, st := range s.stores {
		p, ok := st.(store.Provisioner)
		if !ok {
			continue
		}
		ui.Step("Environments")
		if err := p.EnsureEnvironments(ctx, s.cfg.Environments, flagDryRun, ui.Log); err != nil {
			return err
		}
	}

	s.resolveRuntimeServiceAccount(ctx, env)

	candidates, err := s.candidates()
	if err != nil {
		return err
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("no terminal on stdin; rotate prompts for every value")
	}

	// Which secrets to touch. With --only the picker is skipped entirely, which
	// is what makes the command scriptable for a single-secret emergency
	// rotation.
	chosen := candidates
	if len(flagOnly) == 0 {
		chosen, err = pickSecrets(candidates)
		if err != nil {
			return err
		}
	}
	if len(chosen) == 0 {
		ui.Blank()
		ui.Note("Nothing selected.")
		return nil
	}

	// Everything the validators need is fetched before the first prompt, so
	// typing never blocks on a network call and a validator can stay pure.
	existing := s.existingNames(ctx, env)
	known, err := s.otherEnvironmentFingerprints(ctx, env, chosen)
	if err != nil {
		return err
	}

	values, err := s.collect(env, chosen, known)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		ui.Blank()
		ui.Note("Nothing to rotate.")
		return nil
	}

	if err := s.confirm(env, values, existing); err != nil {
		return err
	}
	if flagDryRun {
		ui.Blank()
		ui.OK("dry run complete; every value validated, nothing written")
		return nil
	}

	return s.write(ctx, env, values)
}

// candidates is the set of secrets in play, honouring --only.
func (s *session) candidates() ([]config.Secret, error) {
	if len(flagOnly) == 0 {
		return s.cfg.Secrets, nil
	}
	var out []config.Secret
	for _, key := range flagOnly {
		sec, ok := s.cfg.Secret(key)
		if !ok {
			return nil, fmt.Errorf("unknown secret key %q; configured: %s",
				key, strings.Join(s.cfg.SecretKeys(), ", "))
		}
		out = append(out, sec)
	}
	return out, nil
}

// resolveRuntimeServiceAccount finds the account that must be able to read a
// newly created Secret Manager secret.
//
// It is taken from a GitHub Actions variable rather than the config, so it
// cannot drift from whatever the deploy workflow actually uses.
func (s *session) resolveRuntimeServiceAccount(ctx context.Context, env config.Environment) {
	if s.gcp == nil || s.gh == nil {
		return
	}
	res, err := shell.Run(ctx, "gh", "variable", "get", "RUN_SERVICE_ACCOUNT",
		"--repo", s.gh.Repo(), "--env", env.GitHubEnv)
	if err != nil {
		res, err = shell.Run(ctx, "gh", "variable", "get", "RUN_SERVICE_ACCOUNT",
			"--repo", s.gh.Repo())
	}
	if err != nil {
		return
	}
	if sa := strings.TrimSpace(res.Stdout); sa != "" {
		s.gcp.SetRuntimeServiceAccount(sa)
		ui.Note("runtime service account %s will be granted read access", sa)
	}
}

// existingNames records which secrets already exist, per store, so the
// confirmation can say "create" or "overwrite" rather than guessing.
func (s *session) existingNames(ctx context.Context, env config.Environment) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, st := range s.stores {
		names, err := st.List(ctx, env)
		if err != nil {
			names = map[string]bool{}
		}
		out[st.ID()] = names
	}
	return out
}

// otherEnvironmentFingerprints maps a digest to the environment that already
// holds that exact value, for every secret in play.
//
// Only a readable store can supply this, and GitHub secrets are write-only, so
// in practice it is Secret Manager doing the work for both. The values read are
// reduced to a digest immediately and not retained; Go strings cannot be wiped,
// so the mitigation is to hold them for as few statements as possible rather
// than to claim they are erased.
func (s *session) otherEnvironmentFingerprints(
	ctx context.Context, env config.Environment, secs []config.Secret,
) (map[string]map[string]string, error) {
	known := map[string]map[string]string{}
	if flagNoCheck {
		return known, nil
	}

	var readable []store.Store
	for _, st := range s.stores {
		if st.Readable() {
			readable = append(readable, st)
		}
	}
	if len(readable) == 0 || len(s.cfg.Environments) < 2 {
		return known, nil
	}

	work := func() {
		for _, sec := range secs {
			known[sec.Key] = map[string]string{}
			for _, other := range s.cfg.Environments {
				if other.Name == env.Name {
					continue
				}
				for _, st := range readable {
					if !sec.UsesStore(st.ID()) {
						continue
					}
					v, err := st.Read(ctx, sec, other)
					if err != nil {
						continue // not found, or no access: nothing to compare
					}
					known[sec.Key][secretval.Fingerprint(v)] = other.Name
				}
			}
		}
	}

	ui.Blank()
	_ = spinner.New().
		Title(" reading what the other environments already hold…").
		Action(work).
		Run()
	return known, nil
}

func pickSecrets(all []config.Secret) ([]config.Secret, error) {
	opts := make([]huh.Option[string], 0, len(all))
	for _, sec := range all {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s — %s", sec.Name, sec.Label), sec.Key))
	}

	var keys []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Which secrets are you rotating?").
			Description("space to toggle · a to select all · enter to continue").
			Options(opts...).
			Value(&keys),
	)).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		return nil, err
	}

	var out []config.Secret
	for _, sec := range all {
		for _, k := range keys {
			if k == sec.Key {
				out = append(out, sec)
			}
		}
	}
	return out, nil
}

// collect prompts for each chosen secret.
func (s *session) collect(
	env config.Environment, secs []config.Secret, known map[string]map[string]string,
) ([]pending, error) {
	var out []pending

	for _, sec := range secs {
		// The targets are printed before anything is asked. This is what
		// replaced a config full of format rules: nothing here guesses whether
		// a value is the right one, so the operator is instead told exactly
		// which name in which store they are about to overwrite, while they
		// still have the dashboard open in front of them.
		s.showTargets(sec, env)

		mode := "type"
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(sec.Label).
				Description(sec.Help).
				Options(
					huh.NewOption("Type or paste a value", "type"),
					huh.NewOption("Generate a random value", "generate"),
					huh.NewOption("Leave it alone", "skip"),
				).
				Value(&mode),
		)).WithTheme(theme()).Run(); err != nil {
			return nil, err
		}

		switch mode {
		case "skip":
			ui.Note("%s — left alone", sec.Name)
			continue

		case "generate":
			v, err := secretval.Generate(0)
			if err != nil {
				return nil, err
			}
			out = append(out, pending{sec: sec, value: v, generated: true})
			ui.OK("%s — generated, %d characters, %s",
				sec.Name, len(v), ui.Fingerprint(secretval.Fingerprint(v)))
			continue
		}

		p, ok, err := promptValue(sec, env, known[sec.Key])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// showTargets prints every name this secret is about to be written to.
func (s *session) showTargets(sec config.Secret, env config.Environment) {
	ui.Step("%s › %s", sec.Name, env.Name)
	for _, st := range s.storesFor(sec) {
		ui.Note("%-22s %s", st.ID(), st.Target(sec, env))
	}
	if sec.Help != "" {
		ui.Note("%-22s %s", "where", sec.Help)
	}
}

// promptValue runs the hidden input for one secret. huh re-prompts on a
// validation error without losing the rest of the session, which is the whole
// reason the checks are pure and everything they need is pre-fetched.
func promptValue(
	sec config.Secret, env config.Environment, known map[string]string,
) (pending, bool, error) {
	var raw string
	var notes []string

	validate := func(in string) error {
		notes = nil
		if strings.TrimSpace(in) == "" {
			return nil // blank means skip, and skipping is always allowed
		}

		clean, n, err := secretval.Sanitize(sec.Name, in)
		if err != nil {
			return err
		}
		notes = n

		// The one cross-check left, and the only one that needs no knowledge of
		// any provider's key format: this exact value already lives in another
		// environment, so it was almost certainly pasted from there.
		if !flagNoCheck {
			if other, dup := known[secretval.Fingerprint(clean)]; dup {
				return fmt.Errorf(
					"%s already holds exactly this value\n"+
						"each environment needs its own credential; re-run with --no-cross-check to override",
					other)
			}
		}
		return nil
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(sec.Name).
			Description("blank to leave it alone").
			EchoMode(huh.EchoModePassword).
			Value(&raw).
			Validate(validate),
	)).WithTheme(theme())

	if err := form.Run(); err != nil {
		return pending{}, false, err
	}

	if strings.TrimSpace(raw) == "" {
		ui.Note("%s — left alone", sec.Name)
		return pending{}, false, nil
	}

	clean, n, err := secretval.Sanitize(sec.Name, raw)
	if err != nil {
		return pending{}, false, err // unreachable: validate already passed
	}
	notes = n

	for _, note := range notes {
		ui.Warn("%s — %s", sec.Name, note)
	}
	ui.OK("%s — %d characters, %s", sec.Name, len(clean),
		ui.Fingerprint(secretval.Fingerprint(clean)))

	return pending{sec: sec, value: clean, notes: notes}, true, nil
}

// confirm shows exactly what is about to be overwritten, then asks.
func (s *session) confirm(
	env config.Environment, values []pending, existing map[string]map[string]bool,
) error {
	rows := [][]string{}
	for _, p := range values {
		for _, st := range s.storesFor(p.sec) {
			target := st.Target(p.sec, env)
			action := "create"
			if existing[st.ID()][lastSegment(target)] {
				action = "overwrite"
			}
			rows = append(rows, []string{p.sec.Name, st.Label(), target, action})
		}
	}

	ui.Step("About to write")
	fmt.Println(ui.Indent(ui.Table(
		[]string{"SECRET", "STORE", "TARGET", "ACTION"}, rows), 2))

	if flagDryRun || flagYes {
		return nil
	}

	ui.Blank()
	proceed := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Write %d value(s) to %s?", len(values), env.Name)).
			Affirmative("Write them").
			Negative("Abort").
			Value(&proceed),
	)).WithTheme(huh.ThemeCharm())
	if err := form.Run(); err != nil {
		return err
	}
	if !proceed {
		return errors.New("aborted; nothing written")
	}
	return nil
}

// write pushes every value to every store, then verifies where it can.
func (s *session) write(ctx context.Context, env config.Environment, values []pending) error {
	ui.Step("Writing")

	failed := 0
	for _, p := range values {
		for _, st := range s.storesFor(p.sec) {
			target := st.Target(p.sec, env)

			if err := st.Write(ctx, p.sec, env, p.value); err != nil {
				ui.Warn("%s %s: FAILED — %v", st.ID(), target, err)
				failed++
				continue
			}

			if !st.Readable() {
				// Nothing can confirm a write-only store beyond the exit
				// status, so the digest of what was sent is the only record
				// that will ever exist. Print it.
				ui.OK("%s %s — set, %s", st.ID(), target,
					ui.Fingerprint(secretval.Fingerprint(p.value)))
				continue
			}

			// A readable store can be verified, and a rotation that silently
			// wrote the wrong bytes is the failure mode worth catching here
			// rather than at the next cold start.
			got, err := st.Read(ctx, p.sec, env)
			switch {
			case err != nil:
				ui.Warn("%s %s — written, but could not be read back: %v", st.ID(), target, err)
				failed++
			case got != p.value:
				ui.Warn("%s %s — READ-BACK MISMATCH: wrote %s, read %s",
					st.ID(), target,
					secretval.Fingerprint(p.value), secretval.Fingerprint(got))
				failed++
			default:
				ui.OK("%s %s — verified, %s", st.ID(), target,
					ui.Fingerprint(secretval.Fingerprint(p.value)))
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d write(s) failed; re-run for the affected secrets", failed)
	}

	s.epilogue(env, values)
	return nil
}

func (s *session) epilogue(env config.Environment, values []pending) {
	ui.Step("Done")

	n := 1
	item := func(format string, a ...any) {
		fmt.Printf("  %d. %s\n", n, fmt.Sprintf(format, a...))
		n++
	}

	if s.gh != nil {
		item("A workflow job only sees these secrets if it declares the environment:\n\n"+
			"       jobs:\n         deploy:\n           environment: %s\n", env.GitHubEnv)
	}
	if s.gcp != nil {
		item("Anything mounting Secret Manager with ':latest' picks the new value up at\n" +
			"     its next revision — instances already running keep the old one. Redeploy\n" +
			"     to cut over now.")
		item("Previous versions are still enabled, so a bad rotation can be rolled back.\n"+
			"     Once the deploy is green, retire them:\n\n"+
			"       gcloud secrets versions list %s --project=%s\n"+
			"       gcloud secrets versions disable <N> --secret=%s --project=%s\n",
			s.gcp.Target(values[0].sec, env), s.gcp.Project(),
			s.gcp.Target(values[0].sec, env), s.gcp.Project())
	}
	item("If this was a leak rather than a scheduled rotation, revoke the old\n" +
		"     credential at its source too. Writing a new one does not disable the old.")

	// A generated value is one nothing else has ever seen, so if anything was
	// already sealed or signed with the old one, it stops working now.
	for _, p := range values {
		if p.generated {
			item("%s was generated here, so nothing else holds the previous value.\n"+
				"     Anything it sealed — sessions, cookies, signed tokens — is invalid\n"+
				"     from the moment %s picks this up.", p.sec.Name, env.Name)
			break
		}
	}
	ui.Blank()
}

func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}
