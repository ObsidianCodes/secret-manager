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

	"github.com/ObsidianCodes/secret-manager/internal/secretval"
	"github.com/ObsidianCodes/secret-manager/internal/shell"
	"github.com/ObsidianCodes/secret-manager/internal/store"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate",
		Short: "Walk every secret in every store and prompt for a new value",
		Long: `Lists what the stores actually hold, and walks it.

Each stop prints the store, the name as that store spells it, and the
environment where there is one, then asks for a value. Enter leaves that one
alone and moves on. Nothing is written until the confirmation at the end.

After you enter a value, secretman offers to reuse it for other secrets you have
not reached yet — which is how one credential gets written to both stores, and
to every environment that shares it, from a single paste. Typing the same value
twice is how two stores end up disagreeing.

There is no list of secrets anywhere in this tool. If a secret does not exist in
a store, it is not walked; create it with ` + "`secretman config add`" + `. If a secret
exists that you did not expect, that is worth knowing, and this is where you
find out.`,
		Example: `  secretman rotate
  secretman rotate --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, entries, err := gather(cmd.Context())
			if err != nil {
				return err
			}
			return s.walk(cmd.Context(), entries)
		},
	}
}

func newHotswapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hotswap",
		Short: "Pick a few secrets from a list and rotate only those",
		Long: `A rotation of exactly the secrets you choose.

Same walk as rotate, over a subset you tick off a list first. This is the
command for the case where one credential leaked at three in the morning and
pressing Enter past thirty others is not a reasonable thing to ask.`,
		Example: `  secretman hotswap
  secretman hotswap --dry-run`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, entries, err := gather(cmd.Context())
			if err != nil {
				return err
			}

			chosen, err := pickEntries(entries,
				"Which secrets are you rotating?", "")
			if err != nil {
				return err
			}
			if len(chosen) == 0 {
				ui.Blank()
				ui.Note("Nothing selected.")
				return nil
			}
			return s.walk(cmd.Context(), chosen)
		},
	}
}

// gather is the shared opening of rotate and hotswap: preflight, then ask every
// store what it holds.
func gather(ctx context.Context) (*session, []store.Entry, error) {
	s, err := loadSession()
	if err != nil {
		return nil, nil, err
	}
	ui.Title("secretman › rotate")
	if err := s.preflight(ctx); err != nil {
		return nil, nil, err
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, nil, errors.New("no terminal on stdin; rotating prompts for every value")
	}

	var entries []store.Entry
	ui.Blank()
	_ = spinner.New().
		Title(" asking the stores what they hold…").
		Action(func() { entries, err = s.enumerate(ctx) }).
		Run()
	if err != nil {
		return nil, nil, err
	}

	if len(entries) == 0 {
		return nil, nil, errors.New("no secrets exist in any configured store\n" +
			"  `secretman config add` creates one")
	}
	return s, entries, nil
}

// pending is one entry the operator has supplied a value for.
type pending struct {
	entry store.Entry
	value string
	// generated records that the value came from crypto/rand rather than a
	// paste, which changes the advice printed at the end.
	generated bool
	// reused records that this value was carried over from another entry in the
	// same session, so the confirmation can show that they will match.
	reused bool
}

// walk is the rotation itself.
func (s *session) walk(ctx context.Context, entries []store.Entry) error {
	// Everything the checks need is fetched before the first prompt, so typing
	// never blocks on a network call.
	known := s.readExisting(ctx, entries)

	assigned := map[string]pending{} // entry ID -> value
	visited := map[string]bool{}

	for _, e := range entries {
		if _, done := assigned[e.ID()]; done {
			// Already filled in by an earlier "use this value here too".
			continue
		}
		visited[e.ID()] = true

		ui.Step("%s", e.Label())
		if st := s.storeFor(e); st != nil && !st.Readable() {
			ui.Note("write-only — the digest below is the only record of this write")
		}

		p, ok, err := promptEntry(e, known)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		assigned[e.ID()] = p

		// Offer the same value to entries not yet reached. This is the only
		// thing that links one store's name to another's, and it is a decision
		// the operator makes in front of the list rather than something this
		// tool infers from a naming convention.
		also, err := s.offerReuse(entries, assigned, visited, p)
		if err != nil {
			return err
		}
		for _, extra := range also {
			assigned[extra.entry.ID()] = extra
		}
	}

	var values []pending
	for _, e := range entries {
		if p, ok := assigned[e.ID()]; ok {
			values = append(values, p)
		}
	}
	if len(values) == 0 {
		ui.Blank()
		ui.Note("Nothing to rotate.")
		return nil
	}

	if err := s.confirm(values); err != nil {
		return err
	}
	if flagDryRun {
		ui.Blank()
		ui.OK("dry run complete; every value checked, nothing written")
		return nil
	}
	return s.write(ctx, values)
}

// readExisting digests what the readable stores already hold, so a value that
// is already in use somewhere can be named at the prompt.
//
// The values read are reduced to a digest immediately and not retained; Go
// strings cannot be wiped, so the mitigation is to hold them for as few
// statements as possible rather than to claim they are erased.
func (s *session) readExisting(ctx context.Context, entries []store.Entry) map[string]string {
	known := map[string]string{} // digest -> label of the entry holding it

	var readable []store.Entry
	for _, e := range entries {
		if st := s.storeFor(e); st != nil && st.Readable() {
			readable = append(readable, e)
		}
	}
	if len(readable) == 0 {
		return known
	}

	work := func() {
		for _, e := range readable {
			v, err := s.storeFor(e).Read(ctx, e)
			if err != nil {
				continue
			}
			known[secretval.Fingerprint(v)] = e.Label()
		}
	}

	ui.Blank()
	_ = spinner.New().
		Title(" reading what is stored now, to recognise a value already in use…").
		Action(work).
		Run()
	return known
}

// promptEntry asks for one value.
func promptEntry(e store.Entry, known map[string]string) (pending, bool, error) {
	// "Leave as is" first, and therefore selected by default: a walk is mostly
	// secrets you are not touching, and Enter should carry you through them
	// without ever arming a write.
	mode := "skip"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(e.Name).
			Description(e.Label()).
			Options(
				huh.NewOption("1. Leave as is", "skip"),
				huh.NewOption("2. Generate a random value", "generate"),
				huh.NewOption("3. Type or paste a value", "type"),
			).
			Value(&mode),
	)).WithTheme(theme()).Run(); err != nil {
		return pending{}, false, err
	}

	switch mode {
	case "skip":
		ui.Note("left as is")
		return pending{}, false, nil

	case "generate":
		v, err := secretval.Generate(0)
		if err != nil {
			return pending{}, false, err
		}
		ui.OK("generated, %d characters, %s", len(v),
			ui.Fingerprint(secretval.Fingerprint(v)))
		return pending{entry: e, value: v, generated: true}, true, nil
	}

	var raw string
	validate := func(in string) error {
		if strings.TrimSpace(in) == "" {
			return nil // blank means skip, and skipping is always allowed
		}
		_, _, err := secretval.Sanitize(e.Name, in)
		return err
	}

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title(e.Name).
			Description("blank to leave it as is").
			EchoMode(huh.EchoModePassword).
			Value(&raw).
			Validate(validate),
	)).WithTheme(theme()).Run(); err != nil {
		return pending{}, false, err
	}

	if strings.TrimSpace(raw) == "" {
		ui.Note("left as is")
		return pending{}, false, nil
	}

	clean, n, err := secretval.Sanitize(e.Name, raw)
	if err != nil {
		return pending{}, false, err // unreachable: validate already passed
	}
	for _, note := range n {
		ui.Warn("%s", note)
	}

	// Told, not refused. Reusing one credential across places is sometimes
	// exactly what is being done here — deliberately, from the prompt below —
	// so this says where else it lives and lets the operator decide.
	if where, dup := known[secretval.Fingerprint(clean)]; dup {
		ui.Warn("this exact value is already stored at %s", where)
	}

	ui.OK("%d characters, %s", len(clean), ui.Fingerprint(secretval.Fingerprint(clean)))
	return pending{entry: e, value: clean}, true, nil
}

// offerReuse asks whether the value just entered belongs anywhere else.
func (s *session) offerReuse(
	all []store.Entry, assigned map[string]pending, visited map[string]bool, p pending,
) ([]pending, error) {
	var candidates []store.Entry
	for _, e := range all {
		if visited[e.ID()] {
			continue
		}
		if _, done := assigned[e.ID()]; done {
			continue
		}
		candidates = append(candidates, e)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Defaults to no: the common case is one credential in one place, and the
	// operator should be able to hold Enter through a walk.
	elsewhere := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Does this same value belong anywhere else?").
			Description("the same credential in another store, or another environment").
			Affirmative("Pick where").
			Negative("Next secret").
			Value(&elsewhere),
	)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}
	if !elsewhere {
		return nil, nil
	}

	chosen, err := pickEntries(candidates,
		"Write this same value to which of these?",
		fmt.Sprintf("value from %s · %s", p.entry.Label(),
			ui.Fingerprint(secretval.Fingerprint(p.value))))
	if err != nil {
		return nil, err
	}

	out := make([]pending, 0, len(chosen))
	for _, e := range chosen {
		out = append(out, pending{entry: e, value: p.value, generated: p.generated, reused: true})
		ui.OK("%s — same value", e.Label())
	}
	return out, nil
}

// pickEntries is the checkbox list used by hotswap and by value reuse.
func pickEntries(entries []store.Entry, title, description string) ([]store.Entry, error) {
	opts := make([]huh.Option[string], 0, len(entries))
	index := map[string]store.Entry{}
	for _, e := range entries {
		opts = append(opts, huh.NewOption(e.Label(), e.ID()))
		index[e.ID()] = e
	}

	var ids []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description("space to toggle · a to select all · enter to continue").
		Options(opts...).
		Value(&ids)
	if description != "" {
		field = field.Description(description + "  ·  space to toggle · enter to continue")
	}

	if err := huh.NewForm(huh.NewGroup(field)).WithTheme(theme()).Run(); err != nil {
		return nil, err
	}

	var out []store.Entry
	for _, e := range entries {
		for _, id := range ids {
			if id == e.ID() {
				out = append(out, index[id])
			}
		}
	}
	return out, nil
}

// confirm shows exactly what is about to be written, then asks.
func (s *session) confirm(values []pending) error {
	rows := make([][]string, 0, len(values))
	for _, p := range values {
		source := "typed"
		switch {
		case p.generated && p.reused:
			source = "generated, reused"
		case p.generated:
			source = "generated"
		case p.reused:
			source = "reused"
		}
		rows = append(rows, []string{
			p.entry.Store, p.entry.Env, p.entry.Name,
			secretval.Fingerprint(p.value), source,
		})
	}

	ui.Step("About to write")
	fmt.Println(ui.Indent(ui.Table(
		[]string{"STORE", "ENVIRONMENT", "NAME", "DIGEST", "SOURCE"}, rows), 2))
	ui.Note("equal digests mean the same value goes to both places")

	if flagDryRun {
		return nil
	}

	ui.Blank()
	proceed := false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Write %d value(s)?", len(values))).
			Affirmative("Write them").
			Negative("Abort").
			Value(&proceed),
	)).WithTheme(theme()).Run(); err != nil {
		return err
	}
	if !proceed {
		return errors.New("aborted; nothing written")
	}
	return nil
}

// write pushes every value to its store, then verifies where it can.
func (s *session) write(ctx context.Context, values []pending) error {
	ui.Step("Writing")

	s.grantRuntimeAccess(ctx)

	failed := 0
	for _, p := range values {
		st := s.storeFor(p.entry)
		if st == nil {
			continue
		}

		if err := st.Write(ctx, p.entry, p.value); err != nil {
			ui.Warn("%s: FAILED — %v", p.entry.Label(), err)
			failed++
			continue
		}

		if !st.Readable() {
			// Nothing can confirm a write-only store beyond the exit status, so
			// the digest of what was sent is the only record that will ever
			// exist. Print it.
			ui.OK("%s — set, %s", p.entry.Label(),
				ui.Fingerprint(secretval.Fingerprint(p.value)))
			continue
		}

		// A readable store can be verified, and a rotation that silently wrote
		// the wrong bytes is the failure worth catching here rather than at the
		// next cold start.
		got, err := st.Read(ctx, p.entry)
		switch {
		case err != nil:
			ui.Warn("%s — written, but could not be read back: %v", p.entry.Label(), err)
			failed++
		case got != p.value:
			ui.Warn("%s — READ-BACK MISMATCH: wrote %s, read %s", p.entry.Label(),
				secretval.Fingerprint(p.value), secretval.Fingerprint(got))
			failed++
		default:
			ui.OK("%s — verified, %s", p.entry.Label(),
				ui.Fingerprint(secretval.Fingerprint(p.value)))
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d write(s) failed; re-run for the affected secrets", failed)
	}

	s.epilogue(values)
	return nil
}

// grantRuntimeAccess finds the account that must be able to read a newly
// created Secret Manager secret.
//
// It is taken from a GitHub Actions variable rather than the config, so it
// cannot drift from whatever the deploy workflow actually uses.
func (s *session) grantRuntimeAccess(ctx context.Context) {
	if s.gcp == nil || s.gh == nil {
		return
	}
	res, err := shell.Run(ctx, "gh", "variable", "get", "RUN_SERVICE_ACCOUNT",
		"--repo", s.gh.Repo())
	if err != nil {
		return
	}
	if sa := strings.TrimSpace(res.Stdout); sa != "" {
		s.gcp.SetRuntimeServiceAccount(sa)
		ui.Note("runtime service account %s will be granted read access", sa)
	}
}

func (s *session) epilogue(values []pending) {
	ui.Step("Done")

	n := 1
	item := func(format string, a ...any) {
		fmt.Printf("  %d. %s\n", n, fmt.Sprintf(format, a...))
		n++
	}

	if s.gh != nil {
		item("A workflow job only sees an environment's secrets if it declares that\n" +
			"     environment:\n\n       jobs:\n         deploy:\n           environment: <name>\n")
	}
	if s.gcp != nil {
		item("Anything mounting Secret Manager with ':latest' picks the new value up at\n" +
			"     its next revision — instances already running keep the old one. Redeploy\n" +
			"     to cut over now.")
		item("Previous versions are still enabled, so a bad rotation can be rolled back:\n\n"+
			"       gcloud secrets versions list <name> --project=%s\n"+
			"       gcloud secrets versions disable <N> --secret=<name> --project=%s\n",
			s.gcp.Project(), s.gcp.Project())
	}
	item("If this was a leak rather than a scheduled rotation, revoke the old\n" +
		"     credential at its source too. Writing a new one does not disable the old.")

	for _, p := range values {
		if p.generated {
			item("%s was generated here, so nothing else holds the previous value.\n"+
				"     Anything it sealed — sessions, cookies, signed tokens — is invalid\n"+
				"     from the moment it is picked up.", p.entry.Name)
			break
		}
	}
	ui.Blank()
}
