# secretman

Rotate project credential. Every store. One environment at a time.

Replace the per-repo bash script. One binary, many project. Project describe itself in `secrets.yaml`.

## Why

Secret rotation break in four way. All four cost more to find than to prevent.

1. **Trailing newline.** Paste carry `\n`. `echo` add one. `gh secret set X < file` inherit one. Every UI show secret hide it. Consumer read newline as part of credential. secretman strip it, and refuse interior one — wrapped paste is mangled paste, guess = silently truncated credential.
2. **Wrong environment.** Paste prod key into staging prompt. Every store accept. Every syntax check pass. Fail later, in prod, with auth error naming nothing. secretman refuse it two way: value's own prefix marker (`sk_live_` say production), and digest match against what other env already hold.
3. **Transposed field.** API key and client id sit next to each other in every dashboard. Swap them, fail at first login. secretman name what you actually pasted.
4. **Half-written rotation.** One store get new value, other keep old. Nobody know which is current. secretman check every store credential BEFORE first keystroke.

Value typed at hidden prompt. Pass to `gh`/`gcloud` on **stdin, never argv** — argv visible in `ps` to every user on machine. Never print. Only digest print.

## Install

One line:

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh | sh
```

Land in `$HOME/.local/bin`. Change with `INSTALL_DIR=`, pin with `VERSION=`:

```
curl -fsSL https://raw.githubusercontent.com/ObsidianCodes/secret-manager/master/install.sh \
  | INSTALL_DIR=/usr/local/bin VERSION=v0.1.0 sh
```

Script pull release tarball for your os/arch, check sha256, drop binary. Nothing else. Read it first if you like — that is the point of it being one file.

Or Go:

```
go install github.com/ObsidianCodes/secret-manager@latest
```

Or local:

```
make build      # -> ./bin/secretman
make install    # -> $GOPATH/bin/secretman
```

Need `gh` and `gcloud` on PATH, both already logged in. secretman reuse their credential. Never ask for token of own, never store one.

## Use

```
secretman doctor              # check config + every store credential, write nothing
secretman init                # create the GitHub Environments
secretman rotate staging      # prompt, validate, write, verify
secretman status              # matrix: which secret exist, which env, which store
secretman verify              # read back: shared value? damaged value?
```

Flags that matter:

| flag | do |
|---|---|
| `--only key,key` | skip picker, rotate just these. For emergency single-secret rotation |
| `--dry-run` | prompt and validate, write nothing |
| `--store gcp` | one store only |
| `--no-cross-check` | allow a value another env already hold. Escape hatch, not default |
| `--yes` | skip confirm |
| `-c path` | config path. Default: walk up from cwd looking for `secrets.yaml` |

## Config

`secrets.yaml` at project root. See `examples/lsr.secrets.yaml`.

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
  - name: development
    gcpSuffix: "-dev"

secrets:
  - key: workos-api-key       # stable id, used by --only
    name: WORKOS_API_KEY      # env var / GitHub secret name
    label: WorkOS secret key
    help: Dashboard › API Keys
    kind: prefixed
    prefix: sk_
    minLength: 20
    conflicts:
      client_: a client id    # paste client id here -> error say what it is
    envMarkers:
      sk_live_: production    # live key in staging -> refused, no network call
```

**Kinds**

| kind | rule |
|---|---|
| `opaque` | any single line, ≥8 char |
| `prefixed` | must start with `prefix` |
| `password` | ≥`minLength` (default 32). `generate: true` offer random value |
| `url` | absolute URL. https, unless env listed in `allowInsecureIn` |

Every kind also honour `minLength`, `conflicts`, `envMarkers`.

Unknown YAML key = error, not ignored. Typo'd key otherwise mean a secret quietly not validated.

## Store

| store | write | read | note |
|---|---|---|---|
| GitHub Actions | env-scoped secret via `gh` | **no** | write-only. Digest of what was sent is only record that will ever exist |
| Google Secret Manager | new version via `gcloud` | yes | read-back verify every write. Old version stay enabled → rollback possible |

GitHub Environment get created if missing. Env-scoped secret cannot be written until env exist. PUT idempotent, so `rotate` do it too — one wasted API call cheaper than rotation failing because somebody deleted an env last week.

GCP: if repo have Actions variable `RUN_SERVICE_ACCOUNT` (env-scoped, or repo-level fallback), secretman grant it `secretmanager.secretAccessor` on new secret. Taken from the variable, not the config, so it cannot drift from what deploy workflow actually use.

## Honest limits

- Go string cannot be wiped. Value read from a store reduce to digest immediately and not retained, but "erased from memory" is not a claim made here.
- 12 hex char of SHA-256 for digest. Only ever confirm equality operator already suspect. Not a security boundary.
- `verify` only work where a store is readable. GitHub alone = nothing to verify.
- Old credential not revoked. Writing new one do not disable old. Go to the provider.

## Dev

```
make test       # go test ./...
make check      # vet + gofmt + test
```

Test cover the parts that must not regress: newline stripping, control-char refusal, transposition naming, per-env URL rules, marker mismatch, config defaults. Test also assert error message never quote the secret.
