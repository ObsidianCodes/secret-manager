# secretman

Rotate project credential. Every store. One walk.

**Keep no list of your secrets.** Config is three field. What exist get read from the store, every run. Nothing to keep in sync, nothing to go stale.

```
secretman init      # ask 3 thing, write .secretman.yaml
secretman rotate    # walk what actually exist, prompt each
secretman hotswap   # tick a few from list, rotate only those
```

## Install

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
```

Land in `$HOME/.local/bin`. Change with `INSTALL_DIR=`, pin with `VERSION=`:

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh \
  | INSTALL_DIR=/usr/local/bin VERSION=v0.1.0 sh
```

Script pull release tarball for your os/arch, check sha256, drop binary. One file — read it first if you like.

Or Go: `go install github.com/ObsidianCodes/secret-manager@latest`
Or local: `make build` → `./bin/secretman`

Need `gh` and `gcloud` on PATH, both logged in. secretman reuse their credential. Never ask for token of own, never store one.

## The config

Whole file. Three field:

```yaml
github:
  repo: ObsidianCodes/lsr     # omit -> gh resolve from cwd

gcp:
  project: lifespanrecords    # project ID, not display name. Often differ
  prefix: lsr-                # everything carrying it is this project's
```

That is it. No secret name, no environment list, no value, no digest. **Commit it.**

Why so small: a list of secret in a file is a second copy of what the store already know, and the copy is the half that rot. Somebody add a secret through the dashboard, file now lie. secretman ask the store instead — `gh secret list`, `gcloud secrets list`, `gh api .../environments`. Cannot be stale, because nothing is remembered.

`prefix` do the real work on GCP side. A GCP project often hold many project secret. Everything with the prefix is your. Everything without is somebody else, and secretman never list it, never walk it, never write it.

Unknown key = error, not ignore.

## Features

**Walk what exist, not what somebody wrote down.** `rotate` ask both store what they hold, then step through it. Every stop print exactly what it about to overwrite:

```
github:WORKOS_API_KEY:staging
  > Type or paste a value
    Generate a random value
    Leave it alone
```

Store, name **as that store spell it**, environment when there is one. GitHub say `WORKOS_API_KEY` in env `staging`. Secret Manager say `lsr-workos-api-key-staging` with no environment, because Secret Manager have no environment. Both shown as they really are. Nothing inferred.

**One paste, many place.** After you enter a value, secretman ask if the same value belong anywhere else, and give you a checkbox list of what you have not reached yet. Tick GitHub staging + GCP, done — one paste, two store, guaranteed identical. Typing same value twice is how two store end up disagreeing about which credential is live.

**Hotswap for the 3am case.** `secretman hotswap` show the full list, you tick the one that leaked, walk only those. No pressing Enter past thirty other.

**Kill the trailing newline.** Paste carry `\n`. `echo` add one. `gh secret set X < file` inherit one. Every UI hide it. Consumer read it as part of credential. secretman strip it — and report the strip, because value written is then not value typed. Also strip pasted `NAME=` and wrapping quote from `.env` paste.

**Refuse mangled paste.** Interior newline or control char mean terminal-wrapped paste. Repair by guessing = silently truncated credential that fail at next cold start. Refuse instead.

**Say where a value already live.** Before first keystroke, secretman digest everything readable. Paste a value that already sit somewhere else, it name the place. **Warn, not refuse** — reusing one credential across place is sometimes exactly what you doing, deliberately, from the prompt above. You decide.

**Generate value nobody issue.** Session sealing key, cookie password. Pick "generate" at any prompt: 32 byte from `crypto/rand`, base64. Never typed, never seen.

**Verify what land.** Readable store get read back after write, digest compared. Mismatch is loud. Write-only store get digest of what was sent printed — only record that will ever exist.

**Find what one environment lack.** `status` compare environment against each other: "staging has CLAIM_TOKEN, production does not". Derived from store state, catch drift both direction, cost nothing. No declared list mean nothing to keep in step — and nothing that can be wrong about what ought to exist.

**Never print a secret.** Value go to `gh`/`gcloud` on **stdin, never argv** — argv visible in `ps` to every user on machine. Hidden prompt. Output only ever show length and `sha256:` first 12 hex.

**No format rule, anywhere.** No `prefix: sk_`, no `minLength`, no expected shape. See below.

**Few flag.** `-c`, `--dry-run`, and two `--force`. That is all of them.

## Usage

### `secretman init`

Ask three thing, write `.secretman.yaml`. Suggest default from `gh repo view` and `gcloud config get-value project`.

Config already exist → **stop and ask**. Warning is specific: changing `repo` or `prefix` point secretman at a *different set of secret*. Nothing get deleted — the one it point at now keep existing, keep working, and stop being visible to this tool entirely.

init create nothing in any store. Environment come from `env add`, secret from `config add`.

| flag | do |
|---|---|
| `--print` | print config to stdout, write nothing |
| `--force` `-f` | replace existing config, no question |

### `secretman rotate`

The walk.

1. Preflight both store — wrong project id, expired login, API not enabled. Fail here, not halfway.
2. Ask store what they hold. This is the list. There is no other list.
3. Digest everything readable, so "already in use" can be named.
4. Per entry: print `store:name:env` → type / generate / skip → hidden prompt → sanitize → **offer to reuse this value elsewhere** → checkbox of untouched entry.
5. Table of every write: STORE, ENVIRONMENT, NAME, DIGEST, SOURCE. Equal digest mean same value going to both place, visible before you commit.
6. Confirm. Write. Read back where store allow.
7. Say what to do next — redeploy, retire old GCP version, revoke at source.

Enter skip. Nothing written until step 6, so ctrl-c before that cost nothing.

Empty store → "no secrets exist in any configured store, `secretman config add` creates one". That is correct, not a bug.

### `secretman hotswap`

Same walk, over a subset you tick first. For when one credential leaked and you not walking thirty.

### `secretman config add`

Create secret that do not exist yet. Enumeration cannot invent, so creation is own command.

Ask name once, then per store where it belong — GitHub: repo-wide and/or which environment (checkbox); GCP: yes/no + the Secret Manager name, prefilled `<prefix><kebab-name>` but editable, so existing naming scheme get adopted not fought. Then value. Then same confirm table as rotate.

### `secretman config rm`

Delete secret **from the store**. Permanent. GCP take every version with it, no rollback after. Tick from list, read the warning, confirm.

Want to stop rotating something without destroying it? Nothing to do — rotate walk what exist, Enter skip.

### `secretman config show` / `path`

Print the file / print which file in effect.

### `secretman env list` / `env add <name>...`

GitHub Environment. Env-scoped secret cannot be written until its environment exist. PUT is idempotent, so naming existing one is not error.

### `secretman status`

What exist where. Matrix per store, secret × environment, `●` / `·`. Then **environment difference** — what one env hold and another lack, both direction.

Presence only. No value read.

### `secretman verify`

Read every secret from every **readable** store:

- **Shared value.** Two entry, one digest. Sometimes correct — that is what a rotation write. Sometimes production key pasted into staging. Name both place, leave judgement to you.
- **Damaged value.** Trailing newline, leading whitespace, interior control char. Written by something that is not this tool — web textarea, `gh secret set X < file`, `echo` in script.

GitHub is write-only, so project on GitHub alone get empty report, not clean bill of health. It say so.

### `secretman doctor`

Preflight, then count what each store hold and list GitHub environment. Write nothing, prompt nothing. Safe in CI, safe while somebody else mid-rotation.

### Global flag

| flag | do |
|---|---|
| `-c path` | config path. Default: walk up from cwd for `.secretman.yaml` |
| `--dry-run` | everything except the write |
| `-h` | help, with example, on every command |

## No format rule, on purpose

secretman never check what a valid value look like. No required prefix, no minimum length, no expected shape, no "this key belong to production".

Rule like that encode a vendor's **current** key format into a file nobody maintain. Vendor ship new format — `ghp_` did not exist before 2021, `sk-proj-` before 2024 — and now your rule refuse a **correct** credential, mid-rotation, when cost is highest and the operator is already stressed. And the rule only help if you already knew the format. If you knew, you were not going to paste the wrong thing.

Showing you `github:WORKOS_API_KEY:staging` before you type beat guessing what is valid.

What survive need no config and cannot go stale:

| check | need |
|---|---|
| strip `NAME=`, quote, whitespace — and report it | nothing |
| refuse control char / wrapped paste | nothing |
| name where this value already live | store state |
| read back after write, compare digest | store state |
| what one environment lack | store state |

## Store

| store | write | read | enumerate | note |
|---|---|---|---|---|
| GitHub Actions | repo + env secret via `gh` | **no** | yes, name only | write-only. Digest of what was sent is only record that will ever exist |
| Google Secret Manager | new version via `gcloud` | yes | yes, filtered by prefix | read-back verify every write. Old version stay enabled → rollback possible |

GCP: if repo have Actions variable `RUN_SERVICE_ACCOUNT`, secretman grant it `secretmanager.secretAccessor` on new secret. Taken from the variable, not the config, so it cannot drift from what deploy workflow actually use.

Rotating do **not** revoke old credential. Writing new one do not disable old. Go to the provider.

## Dev

```
make test       # go test ./...
make check      # vet + gofmt + test
make build      # -> ./bin/secretman
```

Test cover what must not regress: newline stripping, control-char refusal, format-agnostic accept (check that refuse a valid provider key is the failure mode most worth avoiding), environment diff both direction, entry identity across store and env, config round trip, atomic save. Test also assert error message never quote the secret.

Release: tag `v*`, CI cross-build darwin/linux × amd64/arm64, upload tarball + `checksums.txt`.
