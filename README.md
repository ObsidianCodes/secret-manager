# secretman

A small CLI for rotating the credentials a project keeps in GitHub Actions and Google Secret Manager.

```
secretman init      # three questions, writes .secretman.yaml
secretman rotate    # walks what's actually there, asks for each
```

## Install

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
```

Goes to `$HOME/.local/bin`. Set `INSTALL_DIR` to put it elsewhere, `VERSION` to pin a release:

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh \
  | INSTALL_DIR=/usr/local/bin VERSION=v0.2.3 sh
```

The script figures out your OS and architecture, pulls that tarball, checks the sha256 against the published checksums, and drops the binary in place. It's one file and it's short, so give it a read first if that's your habit.

Build it yourself:

```
go install github.com/ObsidianCodes/secret-manager@latest
```

```
make build     # ./bin/secretman
```

You'll need `gh` and `gcloud` installed and logged in. secretman borrows their credentials and never asks for any of its own.

## The config file

```yaml
github:
  repo: ObsidianCodes/lsr

gcp:
  project: lifespanrecords
  prefix: lsr-
```

No secret is named in it. Every command asks the stores directly (`gh secret list`, `gcloud secrets list`, the environments API) and works from whatever comes back.

The `prefix` matters on the GCP side. Secret Manager is one flat namespace per project, and most GCP projects hold secrets for more than one thing. Anything starting with `lsr-` is yours. Everything else secretman won't list, won't touch, won't offer to delete.

An unknown key is an error. Misspell one and the command stops instead of ignoring it.

## Usage

| command | what it does | flags |
|---|---|---|
| `init` | write `.secretman.yaml` | `--print`, `--force` |
| `rotate` | walk everything and prompt for each | |
| `hotswap` | pick a few from a list, walk only those | |
| `config add` | create a secret that doesn't exist yet | |
| `config show` | print the config file | |
| `config path` | print which config is in effect | |
| `delete` | destroy secrets in the stores | `--force` |
| `env list` | list GitHub Environments | |
| `env add <name>...` | create GitHub Environments | |
| `status` | what exists where, and what's missing | |
| `verify` | read values back, report problems | |
| `doctor` | check credentials, change nothing | |

`--dry-run` works everywhere and stops before any write. `-c <path>` points at a specific config instead of searching for one. `delete` is also spelled `rm` or `remove`, and `config rm` still reaches it.

### rotate

```
secretman rotate
```

It asks both stores what they hold, then steps through the answer one entry at a time:

```
github:WORKOS_API_KEY:staging

  > 1. Leave as is
    2. Generate a random value
    3. Type or paste a value
```

Store first, then the secret's name as that store has it, then the environment if there is one. GitHub knows this secret as `WORKOS_API_KEY` inside the `staging` environment. Secret Manager knows the same credential as `lsr-workos-api-key-staging` and has no concept of environments at all. Both get shown as they really are.

"Leave as is" is first and already selected, so you can hold Enter through a long list without changing anything.

Enter a value and you get asked whether it belongs anywhere else, with a checkbox list of everything you haven't reached yet. Tick GitHub staging and the GCP entry, and one paste lands in both, byte for byte. Typing the same key twice into two hidden prompts is how two stores end up disagreeing about which credential is live, and you find out three weeks later when a deploy fails.

Before anything gets written you see a table: store, environment, name, digest, and where the value came from. Matching digests tell you at a glance which entries are about to share a value. Then it asks. Ctrl-C anywhere before that costs nothing.

Afterwards, anything readable gets read back and compared. GitHub secrets can't be read by anyone, including the person who wrote them, so all you get there is the digest of what was sent. That digest is the only record of the write you'll ever get, so it gets printed.

### hotswap

```
secretman hotswap
```

Same walk, but you pick the entries first from a checkbox list. For when one key has leaked and pressing Enter past twenty-nine others isn't reasonable, especially at 3am.

### init

```
secretman init
secretman init --print     # show what it would write, write nothing
secretman init --force     # replace an existing config without asking
```

Asks which stores you use, the repo, the GCP project, and the prefix. The GCP project comes from a list of what `gcloud projects list` sees — you pick one, you never type an id. No projects, no init: make one with `gcloud projects create` first. The repo is suggested from `gh repo view`, so mostly you press Enter.

If a config already exists it stops and asks first. The warning is specific: changing `repo` or `prefix` doesn't edit anything, it points secretman somewhere else entirely. The secrets it was managing keep existing and keep working, they just stop being visible to this tool. Worth understanding before you say yes.

init only looks at the current project. It won't inherit a `.secretman.yaml` from a parent directory the way the other commands do, because a parent's config belongs to a parent's project. It'll mention that one exists and write a new one anyway.

### config add

```
secretman config add
```

Creates a secret that doesn't exist yet. Listing can only find what's already in a store, so creating one needs its own command. It asks for the name once, then per store: for GitHub, repo-wide or which environments; for GCP, whether to include it and what to call it. The Secret Manager name is prefilled from your prefix but you can change it, which matters if you're adopting secrets that already follow some other scheme.

### delete

```
secretman delete
secretman delete --dry-run   # show what would go, delete nothing
secretman delete --force     # skip the confirmation, still shows the list
```

Lists everything, you tick what you want gone, and then it lists what you ticked and asks again. The default is no. Deleting from Secret Manager takes every version with it and leaves nothing to roll back to, and the confirmation says exactly that.

If you just want to stop rotating something without destroying it, do nothing. `rotate` walks what exists and Enter skips.

### env list, env add

```
secretman env list
secretman env add staging
secretman env add staging production development
```

Manage GitHub Environments. An environment-scoped secret can't be written until the environment exists, so new ones get created here first. The underlying call is idempotent, so naming one that already exists costs an API call and nothing else.

### status

```
secretman status
```

Prints a presence matrix per store, then compares your environments against each other. If staging has a `CLAIM_TOKEN` and production doesn't, you'll see it. That's either a deploy waiting to fail or a leftover nobody needs, and both are worth knowing. The comparison runs both ways, and nothing has to be declared anywhere for it to work.

### verify

```
secretman verify
```

Reads values back from whatever's readable and reports two things. Entries sharing a digest, which is sometimes correct (a rotation writes one value to two stores) and sometimes means production's key got pasted into staging. And damaged values: trailing newlines, stray whitespace, control characters, all of which mean something other than this tool did the writing. A trailing newline is invisible in every UI that displays a secret, so nothing else is ever going to show it to you.

On a GitHub-only project verify has nothing to read, and says so rather than reporting that everything is fine when it has no way to know.

### doctor

```
secretman doctor
secretman doctor -c ../other-project/.secretman.yaml
```

Runs the same preflight a rotation runs, counts what each store holds, lists your environments, and stops. Nothing prompted, nothing written. Safe in CI, safe while a colleague is mid-rotation.
