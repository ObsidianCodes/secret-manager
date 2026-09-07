# secretman

Rotate a project's credentials in the stores that hold them. One walk, no local list.

The config is coordinates only: which GitHub repository, which GCP project, which name prefix. What exists is read from the stores on every run, so there is nothing to keep in sync and nothing that can go stale.

```sh
secretman init      # write .secretman.yaml
secretman rotate    # walk what actually exists, prompt for each
secretman hotswap   # tick a few from a list, rotate only those
```

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
```

The binary lands in `$HOME/.local/bin`. Override the directory with `INSTALL_DIR=`, pin a tag with `VERSION=`:

```sh
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | INSTALL_DIR=/usr/local/bin VERSION=v0.1.0 sh
```

The script pulls the release tarball for your OS/arch, checks sha256 against `checksums.txt` when that file is present, and installs `secretman`. It is one file — read it first if you like. Needs `curl` and `tar` on PATH.

Or from source:

```sh
go install github.com/ObsidianCodes/secret-manager@latest
```

```sh
make build && ./bin/secretman
```

secretman shells out to `gh` and `gcloud` for whichever stores the config enables. Both CLIs already hold your login; this tool never asks for a token of its own, and never stores one. Values go to those CLIs on **stdin, never argv** — argv is visible in `ps`.

## Config

At most three fields. Copy is in [`examples/lsr.secretman.yaml`](examples/lsr.secretman.yaml):

```yaml
github:
  repo: ObsidianCodes/lsr # omit → gh resolves from the working directory

gcp:
  project: lifespanrecords # project ID, not display name — they often differ
  prefix: lsr-             # everything carrying it belongs to this project
```

That is the whole file. No secret names, no environment list, no values, no digests. **Commit it.** `secretman init` writes `.secretman.yaml` (or `.secretman.yml`) at the git root when it can find one.

You can configure GitHub only, GCP only, or both. At least one store is required. An unknown YAML key is an error, not ignored.

`prefix` is what makes GCP enumeration safe. A GCP project often holds secrets for more than one thing. Names with the prefix are yours; names without it are never listed, walked, or written.

Changing `repo` or `prefix` points secretman at a different set of secrets. Nothing is deleted — the ones it pointed at before keep existing and keep working. They just stop being visible to this tool.

## Features

**Walk what exists, not what someone wrote down.** `rotate` asks each store what it holds, then steps through that list. Every stop prints the store, the name **as that store spells it**, and the environment when there is one:

```
github:WORKOS_API_KEY:staging
  Type or paste a value
  Generate a random value
  Leave it alone
```

GitHub says `WORKOS_API_KEY` in env `staging`. Secret Manager says `lsr-workos-api-key-staging` with no environment, because Secret Manager has none. Nothing is inferred to be “the same secret.”

**One paste, many places.** After you enter a value, secretman asks whether it belongs anywhere else, and offers a checkbox of entries you have not reached yet. Tick GitHub staging and GCP, done — one paste, two stores, identical by construction. Typing the same value twice is how two stores disagree about which credential is live. The prompt defaults to no, so you can hold Enter through a walk.

**Hotswap for the 3am case.** `secretman hotswap` shows the full list; you tick the ones that leaked and walk only those.

**Kill the trailing newline.** Pastes carry `\n`. `echo` adds one. `gh secret set X < file` inherits one. Every UI hides it; consumers read it as part of the credential. secretman strips surrounding whitespace — and reports the strip, because the value written is then not quite the value typed. It also strips a leading `NAME=` and wrapping quotes from a `.env` paste.

**Refuse a mangled paste.** An interior newline, tab, or other non-printable ASCII usually means the terminal wrapped a long key. Guessing a repair would write a silently truncated credential that fails at the next cold start. It refuses instead.

**Say where a value already lives.** Before the first prompt, readable stores are digested. Paste a value that already sits somewhere else and it names the place. **Warn, not refuse** — reusing one credential across places is sometimes exactly what you are doing, from the prompt above. GitHub is write-only, so this check can only see Secret Manager.

**Generate a value nobody issued.** Session sealing keys, cookie passwords. Pick “generate” at any prompt: 32 bytes from `crypto/rand`, base64. Never typed, never shown.

**Verify what landed.** After a write, readable stores are read back and the digest compared. A mismatch is loud. Write-only stores print the digest of what was sent — the only record that write will ever leave.

**Find what one environment lacks.** `status` compares environments against each other (`github: CLAIM_TOKEN is missing from production`). Derived from store state, both directions, no declared list of what “ought” to exist.

**Never print a secret.** Hidden prompt. Output shows length and `sha256:` plus the first 12 hex of SHA-256. Errors never quote the value.

**No format rules.** No required prefix, no minimum length, no expected shape. See below.

**Few flags.** Persistent `-c` / `--config` and `--dry-run`. `init` has `--print` and `--force`. `config rm` has `--force`.

## Usage

### `secretman init`

Asks which stores to use, then the fields for those stores, and writes `.secretman.yaml`. Suggests defaults from `gh repo view` and `gcloud config get-value project`. A blank GitHub repo lets `gh` resolve it from the working directory.

If a config already exists, init stops and asks before replacing it. `--force` skips the question. `--print` writes the YAML to stdout instead of disk.

init creates nothing in any store. Environments come from `env add`, secrets from `config add`.

### `secretman rotate`

The walk.

1. Preflight both stores — wrong project id, expired login, Secret Manager API not enabled. Fail here, not halfway through a write.
2. Ask the stores what they hold. That is the list. There is no other list.
3. Digest everything readable, so “already in use” can be named.
4. Per entry: print `store:name:env` → type / generate / skip → hidden prompt → sanitize → offer to reuse the value on untouched entries.
5. Table of every pending write: STORE, ENVIRONMENT, NAME, DIGEST, SOURCE. Equal digests mean the same value is going to both places, visible before you confirm.
6. Confirm. Write. Read back where the store allows.
7. Print what to do next — redeploy, leave old GCP versions enabled for rollback, revoke at the source if this was a leak.

Enter skips. Nothing is written until step 6, so aborting before that costs nothing. `--dry-run` runs the same walk and checks, then writes nothing.

If no secrets exist yet, it tells you to create one with `secretman config add`. That is correct, not a bug.

### `secretman hotswap`

The same walk, over a subset you tick first.

### `secretman config add`

Create a secret that does not exist yet. Enumeration cannot invent one, so creation is its own command.

Asks the name once (as an environment variable, e.g. `WORKOS_API_KEY`), then per store: GitHub gets a checkbox of repository-wide and/or which environments; GCP gets yes/no plus the Secret Manager name, prefilled `<prefix><kebab-name>` but editable so an existing naming scheme is adopted rather than fought. Then a value. Then the same confirm table as rotate.

### `secretman config rm`

Delete the secret **from the store**. Permanent. On GCP that takes every version with it; there is no rollback afterwards. Tick from a list, read the warning, confirm. `--force` skips the confirm. Aliases: `remove`, `delete`.

Want to stop rotating something without destroying it? Nothing to do — rotate walks what exists, Enter skips.

### `secretman config show` / `path`

Print the file / print which file is in effect. Discovery walks up from the working directory for `.secretman.yaml` or `.secretman.yml`.

### `secretman env list` / `env add <name>...`

GitHub Environments. An env-scoped secret cannot be written until its environment exists. The underlying PUT is idempotent, so naming one that already exists is not an error.

### `secretman status`

What exists where. A matrix per store (secret × environment, `●` / `·`), then environment differences — names one env holds and another lacks, both directions.

Presence only. No values are read.

### `secretman verify`

Read every secret from every **readable** store:

- **Shared values.** Two entries, one digest. Sometimes correct — that is what a rotation writes. Sometimes a production key was pasted into staging. It names both places and leaves the judgement to you.
- **Damaged values.** Trailing newline, leading/trailing whitespace, interior newline or tab. Written by something other than this tool — a web textarea, `gh secret set X < file`, `echo` in a script.

GitHub is write-only, so a project on GitHub alone gets an empty report, not a clean bill of health. It says so.

### `secretman doctor`

The same preflight a rotation runs, then counts what each store holds and lists GitHub environments. Writes nothing, prompts for nothing. Safe in CI, and safe while someone else is mid-rotation.

## No format rules, on purpose

secretman never checks what a valid value looks like. No required prefix, no minimum length, no expected shape, no “this key belongs to production.”

Rules like that encode a vendor’s **current** key format into a file nobody maintains. Vendors ship new formats — `ghp_` did not exist before 2021, `sk-proj-` before 2024 — and then the rule refuses a **correct** credential, mid-rotation, when the cost is highest. And the rule only helps if you already knew the format.

Showing `github:WORKOS_API_KEY:staging` before you type beats guessing what is valid.

What survives needs no config and cannot go stale:

| check | needs |
| --- | --- |
| strip `NAME=`, quotes, whitespace — and report it | nothing |
| refuse control characters / wrapped paste | nothing |
| name where this value already lives | readable store state |
| read back after write, compare digest | readable store state |
| what one environment lacks | store state |

## Stores

| store | write | read | enumerate | note |
| --- | --- | --- | --- | --- |
| GitHub Actions | repo + env secrets via `gh` | **no** | yes, names only | write-only. Digest of what was sent is the only record |
| Google Secret Manager | new version via `gcloud` | yes | yes, filtered by prefix | read-back verifies every write. Old versions stay enabled, so rollback is possible |

If both stores are configured and the repo has an Actions variable `RUN_SERVICE_ACCOUNT`, secretman grants that account `roles/secretmanager.secretAccessor` when writing a GCP secret. The account is taken from the variable, not the config, so it cannot drift from what the deploy workflow actually uses. A failed grant is reported; the version may already have been added.

Rotating does **not** revoke the old credential. Writing a new one does not disable the old. Go to the provider.

## Dev

```
make test       # go test ./...
make check      # go vet + gofmt -l + test
make build      # → ./bin/secretman
```

Tests cover what must not regress: newline stripping, control-character refusal, format-agnostic accept (refusing a valid provider key is the failure mode most worth avoiding), environment diffs in both directions, entry identity across store and env, config round-trip, atomic save. Errors are asserted never to quote the secret.

Release: tag `v*`. CI cross-builds darwin/linux × amd64/arm64, uploads tarballs and `checksums.txt`.
