# secretman

Rotate project credential. Every store. One environment at a time.

One binary, many project. Project describe itself in `.secretman.yaml` — names only, never a value. Commit that file.

```
secretman init                # wizard write the config, then create the environments
secretman rotate staging      # show target, prompt, write, verify
secretman status              # what exist where
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

Script pull release tarball for your os/arch, check sha256, drop binary. Nothing else. One file — read it first if you like.

Or Go: `go install github.com/ObsidianCodes/secret-manager@latest`
Or local: `make build` → `./bin/secretman`

Need `gh` and `gcloud` on PATH, both logged in. secretman reuse their credential. Never ask for token of own, never store one.

## Features

**Write same credential to many store, one shot.** GitHub Actions environment secret via `gh`. Google Secret Manager version via `gcloud`. One prompt feed both. No more "GCP got the new key, GitHub still on old, nobody know which is live".

**Show you the target before you type.** Every prompt print exactly what it will overwrite:

```
WORKOS_API_KEY › staging
  github    ObsidianCodes/lsr  env:staging
  gcp       lsr-workos-api-key-staging
  where     Dashboard › API Keys
```

No guessing which environment you in. No rule engine pretending to know what a valid key look like.

**Kill the trailing newline.** Paste carry `\n`. `echo` add one. `gh secret set X < file` inherit one. Every UI hide it. Consumer read it as part of credential. secretman strip it — and report the strip, because value written is then not value typed. Also strip pasted `NAME=` and wrapping quote out of `.env` paste.

**Refuse mangled paste.** Value with interior newline or control char is a terminal-wrapped paste. Repair by guessing = silently truncated credential that fail at next cold start. secretman refuse it instead.

**Catch the value you already used somewhere else.** Before first keystroke, secretman read what other environment hold and digest it. Paste staging value into production, get refused, by name. Derive entirely from store state — no config, cannot go stale. `--no-cross-check` override.

**Generate value nobody issue.** Session sealing key, cookie password — thing you invent. Pick "generate" at any prompt, get 32 byte from `crypto/rand`, base64. Never type it, never see it.

**Verify what land.** Readable store get read back after write and digest compared. Mismatch is loud. Write-only store get digest of what was sent printed — only record that will ever exist.

**Audit stored value.** `verify` read every secret from every readable store: two environment sharing one value, value ending in newline, value with control char. `status` print presence matrix — which secret, which env, which store.

**Never print a secret.** Value go to `gh`/`gcloud` on **stdin, never argv** — argv visible in `ps` to every user on machine. Hidden prompt. Output only ever show length and `sha256:` first 12 hex.

**Config you can edit without editing.** `config add|edit|rm` change one secret in place. `init` is bootstrap only, and warn loud before replacing existing config.

**Every command document itself.** `--help` on any command carry a real `Examples` block. `config schema` print every field annotated — for you, and for whatever AI is writing your config.

## Usage

### `secretman init`

Bootstrap. Two step, in order.

1. **Config.** Look for `.secretman.yaml`, walking up from cwd.
   - Not found → wizard. Ask project, store, environment, Secret Manager suffix per env, then secret one by one. Write file at repo root.
   - Found → **stop and ask**. Replacing detach everything the old file describe: nothing get deleted from any store, but secret dropped from file stop being rotated, verified or listed, and tool never mention it again. Confirm say so, and name what get orphaned.
2. **Environments.** Create GitHub Environment declared in config. Environment-scoped secret cannot be written until its environment exist, so this come first.

Write no secret value. Ever.

| flag | do |
|---|---|
| `--force` `-f` | replace existing config with no question |
| `--print` | print config to stdout, write nothing |
| `--dry-run` | do not create environment either |

```
secretman init                # fresh repo
secretman init --print        # see what wizard would write
secretman init --force        # replace, no prompt
```

### `secretman rotate <environment>`

The main one. Order matter, and it is deliberate:

1. Preflight every store — wrong project id, expired login, API not enabled. Fail here, not halfway through.
2. Create missing environment (idempotent).
3. Pick which secret to rotate (skip with `--only`).
4. Read what other environment hold, digest it. One network round, before any typing.
5. Per secret: print target → pick type / generate / skip → hidden prompt → sanitize → cross-check.
6. Show table of every write about to happen: SECRET, STORE, TARGET, CREATE-or-OVERWRITE. Confirm.
7. Write. Read back where store allow. Report digest per write.
8. Print what to do next — redeploy, retire old GCP version, revoke at source.

Blank prompt = leave alone. Nothing written until step 7, so ctrl-c any time before confirm cost nothing.

| flag | do |
|---|---|
| `--only key,key` | skip picker, rotate just these. Emergency path |
| `--dry-run` | everything except the write |
| `--store gcp` | one store only |
| `--no-cross-check` | allow value another env already hold |
| `--yes` `-y` | skip confirm table |

```
secretman rotate staging
secretman rotate production --only workos-api-key
secretman rotate production --dry-run
```

### `secretman status`

Presence matrix, per store. Secret × environment. `●` exist, `·` missing.

Answer the question asked in every incident: is production actually configured, or running on value nobody set since last person left? Presence only — read no value.

```
secretman status
secretman status --store github
```

### `secretman verify`

Read every secret from every **readable** store and report two thing.

- **Shared value.** Two environment holding same digest. One almost certainly got populated by pasting the other. Accepted by every store, fail later in prod, name nothing useful.
- **Damaged value.** Trailing newline, leading whitespace, interior control char. Written by something that is not this tool — web textarea, `gh secret set X < file`, `echo` in a script.

GitHub Actions is write-only, so project on GitHub alone get empty report, not a clean bill of health. It say so.

```
secretman verify
secretman verify --store gcp
```

### `secretman doctor`

Preflight and stop. Config parse, every store credential, what each store can do. Write nothing, prompt nothing — safe in CI, safe while somebody else mid-rotation.

Own command because every failure it catch is one that would otherwise surface halfway through a rotation, one store written and other not.

```
secretman doctor
secretman doctor -c ../other/.secretman.yaml
```

### `secretman config ...`

Edit one secret at a time, in place. Never touch the rest of the file.

| command | do |
|---|---|
| `config add` | wizard, append a secret |
| `config edit [key\|name]` | wizard prefilled. No arg → pick from list |
| `config rm [key\|name]` | remove one, after confirm. `--force` skip |
| `config show` | config as loaded, default filled in. `--raw` for file as written |
| `config path` | which file is in effect |
| `config schema` | every field, annotated. Read no file, need no credential |

`edit` warn when you change key or name: old name stay in every store, holding old value, referenced by nothing. `rm` remove from config only — stored value keep existing and keep working. Revoke at source if you retiring it.

```
secretman config add
secretman config edit WORKOS_API_KEY
secretman config rm workos-claim-token
secretman config schema > .secretman.yaml    # then edit by hand
```

### Global flags

| flag | do |
|---|---|
| `-c path` | config path. Default: walk up from cwd for `.secretman.yaml` |
| `--repo owner/name` | override GitHub repo |
| `--gcp-project id` | override GCP project |
| `--store github,gcp` | limit to these store |
| `--dry-run` | validate, write nothing |
| `-y` | skip confirm |
| `-h` | help, with example, on every command |

## Config

`.secretman.yaml` at repo root. Full annotated reference: `secretman config schema`. Example: `examples/lsr.secretman.yaml`.

```yaml
project: lsr

github:
  repo: ObsidianCodes/lsr     # omit -> gh resolve from cwd

gcp:
  project: lifespanrecords    # project ID, not project NAME. Often differ
  prefix: lsr-                # so many project share one GCP project

environments:
  - name: production
    gcpSuffix: ""             # explicit empty. Prod name carry no suffix
  - name: staging
    gcpSuffix: "-staging"

secrets:
  - key: workos-api-key       # stable id. Used by --only, and as GCP name
    name: WORKOS_API_KEY      # env var / GitHub secret name
    label: WorkOS secret key  # what prompt call it
    help: Dashboard › API Keys  # where to FIND it. Printed at prompt
    stores: [github, gcp]     # omit = every configured store
```

Secret Manager name is `<gcp.prefix><key><env.gcpSuffix>` → `lsr-workos-api-key-staging`. Convention set once, at init. No per-secret override.

Unknown YAML key = error, not ignored. Typo'd key otherwise mean a setting quietly doing nothing.

**No format rule, on purpose.** No `prefix:`, no `minLength:`, no expected shape. Rule like that encode a vendor's *current* key format into a file nobody maintain. Vendor ship new format, rule now refuse a correct credential — mid-rotation, when cost is highest. Showing you the exact target beat guessing what is valid.

What secretman check instead need no config and cannot go stale: newline and control char, and whether another environment already hold this exact value.

## Store

| store | write | read | note |
|---|---|---|---|
| GitHub Actions | env-scoped secret via `gh` | **no** | write-only. Digest of what was sent is only record that will ever exist |
| Google Secret Manager | new version via `gcloud` | yes | read-back verify every write. Old version stay enabled → rollback possible |

GitHub Environment get created if missing. `rotate` do it too — PUT is idempotent, one wasted API call cheaper than rotation failing because somebody deleted an env last week.

GCP: if repo have Actions variable `RUN_SERVICE_ACCOUNT` (env-scoped, or repo-level fallback), secretman grant it `secretmanager.secretAccessor` on new secret. Taken from the variable, not the config, so it cannot drift from what deploy workflow actually use.

Rotating do **not** revoke old credential. Writing new one do not disable old. Go to the provider.

## Dev

```
make test       # go test ./...
make check      # vet + gofmt + test
make build      # -> ./bin/secretman
```

Test cover what must not regress: newline stripping, control-char refusal, format-agnostic accept (a check that refuse a valid provider key is the failure mode this tool most want to avoid), config round trip, explicit-empty `gcpSuffix` survival, atomic save. Test also assert error message never quote the secret.

Release: tag `v*`, CI cross-build darwin/linux × amd64/arm64, upload tarball + `checksums.txt`.
