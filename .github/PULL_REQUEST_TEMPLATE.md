## Description

<!-- Describe the changes introduced in this PR and why they are needed. -->

## Related Issues

<!-- Reference related issues, e.g. Closes #123 -->

## Architectural & Design Checklist

Please confirm your PR adheres to the core architecture principles:

- [ ] **Provider Isolation:** GitLab types/SDK imports remain strictly inside `internal/adapters/gitlab`. No provider leaks into `internal/domain` or `internal/policy/*`.
- [ ] **Pure Policies:** Policy evaluations are deterministic and pure (no I/O, no network, no logging; uses `domain.Clock` rather than `time.Now()`).
- [ ] **Plan-Before-Execute:** Destructive actions are kept strictly behind a two-stage plan/execution model.
- [ ] **Safe Defaults:** Unknown or missing data is modeled explicitly as `domain.Unknown()` and never treated as "safe to delete".
- [ ] **Tests:**
  - [ ] Unit tests with `domain.FixedClock` or table-driven cases added/updated.
  - [ ] Adapter tests use `httptest.Server` rather than live remote instances.
  - [ ] `make check` (`make fmt-check`, `make tidy-check`, `make vet`, `make test-race`, `make lint`) passes cleanly.
- [ ] **Documentation:** README and/or `docs/architecture.md` updated if operator-visible behavior or flags changed.
