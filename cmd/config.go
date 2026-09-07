package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/ObsidianCodes/secret-manager/internal/config"
	"github.com/ObsidianCodes/secret-manager/internal/ui"
)

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit .secretman.yaml",
		Long: `Changes one secret at a time, in place.

init writes the whole file and replacing it detaches everything it described.
These subcommands do not: they load the existing config, change the one secret
named, and write it back with every other entry untouched.

The file is edited raw. Defaults the loader would fill in — an environment's
Secret Manager suffix, a password's minimum length — are not written to disk by
these commands, because a default written down is a decision nobody made and it
stops tracking the default it came from.`,
		Example: `  secretman config show          # the config as loaded, defaults filled in
  secretman config add           # wizard, append a secret
  secretman config edit API_KEY  # wizard, prefilled from the existing entry
  secretman config rm API_KEY    # remove one, after confirming
  secretman config schema        # every field, annotated`,
	}
	c.AddCommand(
		newConfigShowCmd(),
		newConfigAddCmd(),
		newConfigEditCmd(),
		newConfigRemoveCmd(),
		newConfigPathCmd(),
		newConfigSchemaCmd(),
	)
	return c
}

// --------------------------------------------------------------- show

func newConfigShowCmd() *cobra.Command {
	var raw bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Print the configuration",
		Long: `Prints the config as secretman understands it, with every default filled in.

Useful for exactly one question: which Secret Manager name does this actually
resolve to? --raw prints the file as written instead.`,
		Example: `  secretman config show
  secretman config show --raw`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath()
			if err != nil {
				return err
			}
			if raw {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				fmt.Print(string(b))
				return nil
			}
			cfg, err := config.Load(path, "")
			if err != nil {
				return err
			}
			out, err := config.Marshal(cfg)
			if err != nil {
				return err
			}
			fmt.Print(string(out))
			return nil
		},
	}
	c.Flags().BoolVar(&raw, "raw", false, "print the file as written, without defaults")
	return c
}

// --------------------------------------------------------------- add

func newConfigAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a secret to the config",
		Long: `Asks for one secret and appends it, leaving every existing entry alone.

Adding a secret writes no value anywhere. The new entry is picked up by the next
rotate, where it will be prompted for like any other.`,
		Example: `  secretman config add`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return editConfig(func(cfg *config.Config) (string, error) {
				sec, err := wizardSecret(cfg, nil)
				if err != nil {
					return "", err
				}
				cfg.PutSecret(sec)
				return fmt.Sprintf("added %s (%s)", sec.Name, sec.Key), nil
			})
		},
	}
}

// --------------------------------------------------------------- edit

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit [key|name]",
		Short: "Change one secret in the config",
		Long: `Re-runs the secret wizard with every field prefilled from the existing entry.

Editing changes what is checked and where the value is written. It does not
touch any stored value: renaming a secret leaves the old name in every store,
holding the old value, referenced by nothing.

With no argument, the secret is picked from a list.`,
		Example: `  secretman config edit
  secretman config edit workos-api-key
  secretman config edit WORKOS_API_KEY`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return editConfig(func(cfg *config.Config) (string, error) {
				sec, err := pickSecret(cfg, args, "Edit which secret?")
				if err != nil {
					return "", err
				}
				updated, err := wizardSecret(cfg, &sec)
				if err != nil {
					return "", err
				}
				// The key is the identity, so a changed one is a move: drop the
				// old entry rather than leaving a duplicate behind.
				if updated.Key != sec.Key {
					cfg.RemoveSecret(sec.Key)
					ui.Warn("key %s → %s; --only and any script naming the old key must change",
						sec.Key, updated.Key)
				}
				if updated.Name != sec.Name {
					ui.Warn("name %s → %s; the old name still exists in every store, "+
						"still holding the old value", sec.Name, updated.Name)
				}
				cfg.PutSecret(updated)
				return fmt.Sprintf("updated %s (%s)", updated.Name, updated.Key), nil
			})
		},
	}
}

// --------------------------------------------------------------- rm

func newConfigRemoveCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:     "rm [key|name]",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a secret from the config",
		Long: `Drops one secret from the config. Nothing is deleted from any store.

The stored value keeps existing, keeps working, and stops being rotated,
verified or listed by this tool. If the credential is being retired rather than
just unmanaged, revoke it at its source too — this command cannot.`,
		Example: `  secretman config rm
  secretman config rm workos-claim-token
  secretman config rm workos-claim-token --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return editConfig(func(cfg *config.Config) (string, error) {
				sec, err := pickSecret(cfg, args, "Remove which secret?")
				if err != nil {
					return "", err
				}
				if len(cfg.Secrets) == 1 {
					return "", fmt.Errorf("%s is the only secret configured; "+
						"a config with none does not load", sec.Key)
				}

				if !force {
					if err := requireTTY("config rm"); err != nil {
						return "", err
					}
					ui.Blank()
					ui.Warn("%s stays in every store, holding its current value.", sec.Name)
					ui.Warn("It stops being rotated, verified or listed by secretman.")
					ui.Blank()

					ok := false
					if err := huh.NewForm(huh.NewGroup(
						huh.NewConfirm().
							Title(fmt.Sprintf("Remove %s from the config?", sec.Name)).
							Affirmative("Remove it").
							Negative("Keep it").
							Value(&ok),
					)).WithTheme(theme()).Run(); err != nil {
						return "", err
					}
					if !ok {
						return "", nil
					}
				}

				cfg.RemoveSecret(sec.Key)
				return fmt.Sprintf("removed %s (%s)", sec.Name, sec.Key), nil
			})
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "do not ask")
	return c
}

// --------------------------------------------------------------- path

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path of the config in effect",
		Long: `Prints where the config was found, and nothing else.

Which file is in effect depends on the working directory, so it is worth being
able to ask.`,
		Example: `  secretman config path`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := configPath()
			if err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
}

// --------------------------------------------------------------- schema

func newConfigSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print an annotated reference config",
		Long: `Prints every field the config accepts, with a comment on each.

Reads no file and needs no credentials, so it works before anything is set up
and is the fastest way to answer "what can this key be?" — for a person or for
whatever is generating the file.`,
		Example: `  secretman config schema
  secretman config schema > .secretman.yaml   # then edit by hand`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(schemaDoc)
			return nil
		},
	}
}

// --------------------------------------------------------------- plumbing

// editConfig loads the config raw, hands it to f, and writes it back if f
// changed something. f returns the line to print, or "" for "nothing changed".
func editConfig(f func(*config.Config) (string, error)) error {
	if err := requireTTY("config editing"); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadRaw(path, "")
	if err != nil {
		return err
	}

	ui.Title("%s › config", cfg.Project)
	ui.Note("config %s", path)

	what, err := f(cfg)
	if err != nil {
		return err
	}
	if what == "" {
		ui.Blank()
		ui.Note("Nothing changed.")
		return nil
	}

	if err := config.Save(cfg, path); err != nil {
		return err
	}
	ui.Blank()
	ui.OK("%s", what)
	ui.Note("wrote %s", path)
	ui.Blank()
	return nil
}

func configPath() (string, error) {
	if flagConfig != "" {
		return flagConfig, nil
	}
	return config.Discover("")
}

// pickSecret resolves an argument to a secret, by key or by name, or asks.
func pickSecret(cfg *config.Config, args []string, title string) (config.Secret, error) {
	if len(args) == 1 {
		want := args[0]
		if sec, ok := cfg.Secret(want); ok {
			return sec, nil
		}
		for _, sec := range cfg.Secrets {
			if strings.EqualFold(sec.Name, want) {
				return sec, nil
			}
		}
		return config.Secret{}, fmt.Errorf("no secret %q; configured: %s",
			want, strings.Join(cfg.SecretKeys(), ", "))
	}

	opts := make([]huh.Option[string], 0, len(cfg.Secrets))
	for _, sec := range cfg.Secrets {
		opts = append(opts, huh.NewOption(
			fmt.Sprintf("%s — %s", sec.Name, sec.Key), sec.Key))
	}

	var key string
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Options(opts...).Value(&key),
	)).WithTheme(theme()).Run(); err != nil {
		return config.Secret{}, err
	}
	sec, _ := cfg.Secret(key)
	return sec, nil
}
