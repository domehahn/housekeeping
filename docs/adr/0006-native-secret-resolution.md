# ADR 0006: Generic secret references with native OS keychain support

## Status

Accepted

## Context

GitLab credentials were previously referenced only through `token_env`. That
is appropriate for CI but forces workstation users to place a privileged token
in process environment state. Adding OS-specific logic directly to the GitLab
adapter or CLI would couple authentication storage to provider behavior and
make tests dependent on a user's real credential store.

The established YAML and `--token-env` interface must remain compatible. A
misconfigured source must fail closed and must not select a different ambient
credential.

## Decision

Introduce `internal/secrets` with a generic `Reference`, strongly typed source,
and context-aware `Resolver`. A registry dispatches once to an environment or
native-keychain backend and never falls back. Backends expose narrow injectable
interfaces; unit tests use fakes and never touch the real OS keychain.

Use `github.com/zalando/go-keyring` behind the internal `Keyring` interface. It
is MIT licensed, supports macOS Keychain, Linux/BSD Secret Service, and Windows
Credential Manager, has a small API, works with the project's Go version, and
keeps keychain concepts out of provider adapters.

Expose explicit `auth login`, `auth status`, and `auth logout` commands for
native-keychain entries. Login obtains the value only through an interactive,
no-echo terminal prompt; no token flag or piped-stdin input is accepted. Status
reports only whether a non-empty credential exists. Logout is idempotent when
the entry is already absent. Backend failures are sanitized before reaching
CLI output. Normal resolver and provider construction never write credentials.

Add a structured `provider.gitlab.token` block. Normalize legacy `token_env`
and the existing `--token-env` flag into an environment reference. Reject both
YAML forms together, unknown sources, irrelevant source fields, missing
identifiers, literal token values, and unknown fields.

Inject the generic resolver into the provider factory. The factory resolves
once and passes only the value to the GitLab adapter. `config validate` uses the
same resolver but performs no GitLab network request. No resolved value is
logged, serialized, persisted, or cached by the resolver layer.

## Consequences

- Interactive users can rely on their OS credential store; CI retains the
  environment-variable workflow.
- Secret backends are isolated from providers and can be extended without
  changing adapter contracts.
- Keychain availability, unlock prompts, and access controls follow the host
  OS. Linux/BSD requires a working Secret Service session.
- Explicit configuration is required; there is intentionally no convenience
  fallback between environment and keychain sources.
- Keychain mutations are explicit auth-command operations and are covered by
  fake-backed unit tests; the test suite never touches a user's native store.
- The legacy configuration remains supported but structured syntax is the
  preferred form for new deployments.
