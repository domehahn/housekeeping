# Contributing

## Workflow

1. Branch from `main`.
2. Make your change. Keep GitLab-specific code inside
   `internal/adapters/gitlab`; keep `internal/domain` and
   `internal/policy/*` free of provider-specific types and I/O.
3. Add or update tests. Policy/domain logic should be tested with a
   `domain.FixedClock` and table-driven cases, never `time.Now()`. Adapter
   changes should be tested against `httptest.Server`, never a live GitLab
   instance.
4. Run the full check suite before opening a merge request:

   ```bash
   make fmt
   make vet
   make test
   make test-race
   make lint   # requires golangci-lint installed locally
   ```

5. Open a merge request describing the change and, if it affects behavior
   an operator would notice, update the README and/or
   `docs/architecture.md`.

## Design constraints (please read before large changes)

- **No GitLab types outside `internal/adapters/gitlab`.** If you find
  yourself importing `gitlab.com/gitlab-org/api/client-go` anywhere else,
  that's a sign the abstraction needs adjusting, not that the import is
  fine this one time.
- **Policies are pure.** No network calls, no `time.Now()`, no logging
  inside `internal/policy/*`. If a policy needs "now", it takes a
  `domain.Clock`.
- **Destructive operations stay behind plan/execute.** Do not add a CLI
  command that discovers and mutates in the same invocation.
- **Unknown is not the same as inactive.** Any new activity/attribute
  source must model "the provider didn't tell us" as
  `domain.Unknown()`, not as a zero value that happens to look like
  "never happened."
- **Small interfaces.** Add capability interfaces to `internal/provider`
  scoped to what a use case actually needs, rather than growing a large
  `Provider` interface.
- **Verify against current GitLab API docs.** Do not guess at field names,
  endpoint behavior, or permission requirements - check
  https://docs.gitlab.com/api/ and cite what you found in a code comment
  when it's non-obvious (see existing comments in
  `internal/adapters/gitlab` for the expected style).

## Adding a new provider

See the README's "Adding another Provider" section. In short: implement
the `internal/provider` interfaces you can support in a new
`internal/adapters/<name>` package, map that provider's API types to
`internal/domain` types in a `mapper.go`, translate its errors to
`provider.Error` in an `errors.go`, and add one case to
`internal/providerfactory.New`. Do not touch `internal/app` or
`internal/policy`.

## Reporting security issues

Please do not open a public issue for a security vulnerability. See the
Security Considerations section of the README for the threat model this
project already accounts for, and describe how your finding falls outside
it when reporting privately.
