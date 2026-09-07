package cmd

// schemaDoc is the annotated reference config printed by `config schema`.
//
// Kept as one literal rather than generated from the struct tags: the useful
// half of it is why a field exists and when to reach for it, and that is not
// recoverable from a type.
const schemaDoc = `# .secretman.yaml — every field there is.
#
# Found by walking up from the working directory, so any subdirectory of the
# repository works. .secretman.yml is also accepted. Override with --config.
#
# An unknown key is an error, not a warning: a typo'd key would otherwise be a
# setting that silently does nothing.
#
# Holds names. Never a value, never a digest. Commit it.
#
# There is deliberately no way to say what a valid value looks like — no
# required prefix, no minimum length, no expected format. Rules like that
# encode a provider's current key format into a file nobody maintains, and
# when the provider changes format the rule refuses a correct credential in
# the middle of a rotation. Instead, rotate prints the store, environment and
# name it is about to overwrite before it asks for anything.

project: lsr                    # required. Names the project in output.

# ---------------------------------------------------------------- stores
# At least one is required. Every secret goes to every store listed here,
# unless a secret narrows it with its own "stores:" key.

github:
  repo: ObsidianCodes/lsr       # owner/name. Omit to let gh resolve the
                                # repository from the working directory.

gcp:
  project: lifespanrecords      # the project ID, which is not the display
                                # name, and is frequently not what you expect.
  prefix: lsr-                  # prepended to every Secret Manager name, so
                                # several projects can share one GCP project
                                # without colliding. The full name is
                                # <prefix><key><gcpSuffix>.

# ---------------------------------------------------------------- environments
# At least one is required.

environments:
  - name: production            # required, unique.
    githubEnv: production       # the GitHub Environment name. Defaults to name.
    gcpSuffix: ""               # appended to the Secret Manager name. Secret
                                # Manager has no notion of environments, so the
                                # environment lives in the name. An explicit ""
                                # is legitimate and conventionally production's:
                                # the name most consumers reference carries no
                                # suffix. Omitting the key entirely defaults to
                                # "-<name>", which is NOT the same as "".
  - name: staging
    gcpSuffix: -staging
  - name: development
    gcpSuffix: -dev

# ---------------------------------------------------------------- secrets
# At least one is required.

secrets:
  - key: workos-api-key         # required, unique. Stable id. Used by --only,
                                # and as the Secret Manager name between the
                                # prefix and the suffix. Renaming it orphans
                                # the stored value under the old name.
    name: WORKOS_API_KEY        # required, unique. The environment variable
                                # and GitHub secret name.
    label: WorkOS secret key    # what the prompt calls it. Defaults to name.
    help: Dashboard › API Keys  # printed at the prompt. Put where to FIND the
                                # value here — that is what the operator needs
                                # while they are looking at a dashboard.
    stores: [github, gcp]       # which stores hold this one. Omit for all of
                                # the configured stores, which also means a
                                # store added later picks it up automatically.

  - key: workos-cookie-password
    name: WORKOS_COOKIE_PASSWORD
    label: Session cookie sealing key
    # Nothing marks this one as generatable: rotate offers to generate a random
    # value at every prompt, so there is nothing to declare.
`
