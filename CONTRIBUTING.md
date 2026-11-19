# Contributing

## Workflow

1. Branch from `main` (or `rewrite/rust`).
2. Make your change. Keep GitLab-specific code inside
   `src/adapters/gitlab`; keep `src/domain` and
   `src/policy` free of provider-specific types and I/O.
3. Add or update tests. Policy/domain logic should be tested with a
   `domain::FixedClock` and table-driven cases, never non-deterministic wall clock time.
4. Run the full check suite before opening a pull request:

   ```bash
   make fmt-check   # cargo fmt -- --check
   make clippy      # cargo clippy -- -D warnings
   make test        # cargo test
   # or all-in-one:
   make check
   ```

5. Open a pull request describing the change and, if it affects behavior
   an operator would notice, update the README and/or
   `docs/architecture.md`. Complete the pull request checklist.

## Design constraints (please read before large changes)

- **No GitLab types outside `src/adapters/gitlab`.** If you find
  yourself leaking provider structs into `domain` or `policy`,
  that's a sign the abstraction needs adjusting.
- **Policies are pure.** No network calls, no system clock calls, no logging
  inside `src/policy`. If a policy needs "now", it takes a `&dyn Clock`.
- **Destructive operations stay behind plan/execute.** Do not add a CLI
  command that discovers and mutates in the same invocation.
- **Unknown is not the same as inactive.** Any new activity/attribute
  source must model "the provider didn't tell us" as
  `ActivityStatus::Unknown`, not as a zero value that happens to look like
  "never happened."
- **Small interfaces.** Keep provider traits scoped to what use cases actually need.
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

Please do not open a public issue for a security vulnerability. See [SECURITY.md](SECURITY.md) and [docs/threat-model.md](docs/threat-model.md) for reporting guidelines and our security threat model.
