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

Prefer to build it yourself:

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

That's the whole thing. Commit it.

No secret is named in it. Every command asks the stores directly (`gh secret list`, `gcloud secrets list`, the environments API) and works from whatever comes back.

The `prefix` matters on the GCP side. Secret Manager is one flat namespace per project, and most GCP projects hold secrets for more than one thing. Anything starting with `lsr-` is yours. Everything else secretman won't list, won't touch, won't offer to delete.

An unknown key is an error. Misspell one and the command stops instead of ignoring it.

## Rotating

`rotate` asks both stores what they hold, then steps through it one at a time:

```
github:WORKOS_API_KEY:staging

  > 1. Leave as is
    2. Generate a random value
    3. Type or paste a value
```

Store, then the name spelled the way that store spells it, then the environment if there is one. GitHub knows this secret as `WORKOS_API_KEY` inside the `staging` environment. Secret Manager knows the same credential as `lsr-workos-api-key-staging` and has no concept of environments at all. Both get shown as they really are.

"Leave as is" is first and selected, so you can hold Enter through a long walk without ever arming a write.

Enter a value and you get asked whether it belongs anywhere else, with a checkbox list of everything you haven't reached yet. Tick GitHub staging and the GCP entry, and one paste lands in both, byte for byte. This is the part worth having. Typing the same key twice into two hidden prompts is how two stores end up disagreeing about which credential is live, and you find out three weeks later when a deploy fails.

Before anything gets written you see a table: store, environment, name, digest, and where the value came from. Matching digests tell you at a glance which entries are about to share a value. Then it asks. Ctrl-C anywhere before that costs nothing.

Afterwards, anything readable gets read back and compared. GitHub secrets can't be read by anyone, including the person who wrote them, so all you get there is the digest of what was sent. That digest is the only record that will ever exist of that write, which is why it's printed.

### hotswap

Same walk, but you pick the entries first from a checkbox list. For when one key leaked and pressing Enter past twenty-nine others isn't a reasonable thing to ask of anyone, least of all at 3am.

## Other commands

**`init`** asks which stores you use, the repo, the GCP project, and the prefix. It suggests answers from `gh repo view` and `gcloud config get-value project`, so mostly you press Enter.

If a config already exists it stops and asks first. The warning is specific: changing `repo` or `prefix` doesn't edit anything, it points secretman somewhere else entirely. The secrets it was managing keep existing and keep working, they just stop being visible to this tool. Worth understanding before you say yes.

init only looks at the current project. It won't inherit a `.secretman.yaml` from a parent directory the way the other commands do, because a parent's config belongs to a parent's project. It'll mention that one exists and write a new one anyway.

**`config add`** creates a secret that doesn't exist yet. Enumeration can only find what's there, so creation needs its own command. It asks for the name once, then per store: for GitHub, repo-wide or which environments; for GCP, whether to include it and what to call it. The Secret Manager name is prefilled from your prefix but you can change it, which matters if you're adopting secrets that already follow some other scheme.

**`delete`** (also `rm`) lists everything, you tick what you want gone, and then it asks again with the ticked entries printed outside the picker. The default is no. Deleting from Secret Manager takes every version with it and there's nothing to roll back to afterwards, so the confirmation says so in those words.

If you just want to stop rotating something without destroying it, do nothing. `rotate` walks what exists and Enter skips.

**`env list` / `env add`** manage GitHub Environments. An environment-scoped secret can't be written until the environment exists, so new ones get created here first. The underlying call is idempotent, so naming one that already exists costs an API call and nothing else.

**`status`** prints a presence matrix per store, then compares your environments against each other. If staging has a `CLAIM_TOKEN` and production doesn't, you'll see it. That's either a deploy waiting to fail or a leftover nobody needs, and both are worth knowing. It works in both directions and needs nothing declared anywhere.

**`verify`** reads values back from whatever's readable and reports two things. Entries sharing a digest, which is sometimes correct (a rotation writes one value to two stores) and sometimes means production's key got pasted into staging. And damaged values: trailing newlines, stray whitespace, control characters, all of which mean something other than this tool did the writing. A trailing newline is invisible in every UI that displays a secret, so nothing else is ever going to show it to you.

On a GitHub-only project verify has nothing to read and says so, rather than reporting a clean bill of health it can't actually vouch for.

**`doctor`** runs the same preflight a rotation runs, counts what each store holds, lists your environments, and stops. Nothing prompted, nothing written. Safe in CI, safe while a colleague is mid-rotation.

## What it checks, and what it deliberately doesn't

Paste handling is the boring part that earns its keep. Values get stripped of surrounding whitespace, a leading `NAME=` from a copied `.env` line, and wrapping quotes. Anything stripped is reported, because at that point the value written isn't the value you typed and you should know. Control characters and interior newlines are refused outright rather than cleaned up, since that's almost always a terminal that hard-wrapped a long key, and guessing at the repair would write a truncated credential that fails at the next cold start instead of here.

What secretman won't do is judge whether a value is the right *kind* of thing. There's no required prefix, no minimum length, no "this key looks like production".

Earlier versions had all of that. You'd write `prefix: sk_` in the config and it would refuse anything else. The problem is that you've now written a vendor's current key format into a file nobody maintains. `ghp_` didn't exist before 2021. `sk-proj-` didn't exist before 2024. When the format changes your rule starts rejecting perfectly good credentials, and it does it mid-rotation, which is exactly when nobody has patience for arguing with their own tooling. The check also only helps if you already knew the format, and if you knew it you probably weren't about to paste the wrong thing.

Showing you `github:WORKOS_API_KEY:staging` before you type turned out to be worth more than any rule.

There is one cross-check left, and it needs no configuration: before the first prompt, secretman digests everything readable. If you paste a value that already lives somewhere, it tells you where. That's a warning and not a refusal, because reusing one credential across two stores is a thing this tool now helps you do on purpose.

| check | needs |
|---|---|
| strip `NAME=`, quotes, whitespace, and say so | nothing |
| refuse control characters and wrapped pastes | nothing |
| tell you where this value already lives | store state |
| read back and compare after writing | store state |
| what one environment is missing | store state |

Nothing in that column can go stale.

## The two stores

| | GitHub Actions | Google Secret Manager |
|---|---|---|
| write | repo and environment secrets via `gh` | new version via `gcloud` |
| read | no, ever | yes |
| list names | yes | yes, filtered by prefix |
| verify a write | impossible, digest only | read-back comparison |
| rollback | none | previous versions stay enabled |

Values reach both tools on stdin, never as arguments. Anything on a command line is visible in `ps` to every other user on the machine, and this is one of the few places that actually matters.

If your repo has an Actions variable called `RUN_SERVICE_ACCOUNT`, secretman grants it `secretmanager.secretAccessor` on any secret it creates in GCP. It reads that from the variable rather than the config specifically so it can't drift from whatever your deploy workflow is really using.

One thing rotation doesn't do: revoke anything. Writing a new credential doesn't disable the old one. If this was a leak rather than a scheduled rotation, go turn the old one off at the provider.

## Development

```
make test     # go test ./...
make check    # vet, gofmt, test
make build    # ./bin/secretman
```

The tests cover the things that would be embarrassing to break: newline stripping, control-character refusal, accepting credentials in any format at all (a check that rejects a valid provider key is the failure mode I care most about avoiding), environment diffing in both directions, entry identity across stores and environments, config round-tripping, and the atomic save. There's also one asserting no error message ever quotes the secret it's complaining about.

Every push to master ships. CI works out the next patch tag, runs `make check`, cross-compiles for darwin and linux on amd64 and arm64, and publishes tarballs with checksums. Currently on the `0.2.x` series and staying there until the config format settles; bumping the minor would imply a compatibility promise I'm not making yet.

To cut a specific version, push the tag yourself or run the workflow with a tag input.
