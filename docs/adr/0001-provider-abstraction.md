# 0001: Provider abstraction via small capability interfaces

## Status

Accepted

## Context

scm-cleaner must support GitLab first, with GitHub, Bitbucket, Azure
DevOps, Gitea, and Forgejo as plausible future targets. The business logic
(policies, planning, execution, safety guards) must not need to change, or
even be recompiled with provider-specific knowledge, when a new provider is
added.

A naive approach is a single large `Provider` interface with every
operation (`GetProjects`, `DeleteProject`, `GetUsers`, `DeleteUser`, ...).
This has two problems: (1) every adapter must implement every method even
if a given provider cannot support some of them (e.g. Bitbucket has a
different membership model than GitLab), and (2) every consumer depends on
the whole surface even if it only needs one operation, making unit tests
require a much larger fake than necessary.

## Decision

Define many small, single-purpose interfaces in `internal/provider`
(`ProjectReader`, `ProjectDeleter`, `ProjectArchiver`, `GroupMemberReader`,
`GroupMemberRemover`, `UserBlocker`, `CurrentUserResolver`,
`ScopeResolver`, `CapabilitiesReporter`, `InfoReporter`,
`ProjectGetter`/`UserGetter`). `provider.Client` aggregates them as a
convenience type for wiring the CLI, but `internal/app` functions declare
only the specific interfaces they need as parameters.

Provider capability is additionally reported at runtime via
`provider.Capabilities` (`Support` = supported / unsupported /
requires-admin / requires-owner / unknown), surfaced through
`scm-cleaner provider capabilities`, so an operator can see before running
anything which operations are realistically available with their
credentials on their instance.

## Consequences

- Adding GitHub means implementing the interfaces GitHub can support in a
  new `internal/adapters/github` package and adding one case to
  `internal/providerfactory.New`. No changes to `internal/app`,
  `internal/policy`, or `internal/cli` command bodies.
- Test doubles for `internal/app` functions only need to implement the
  handful of methods a given test actually exercises (see
  `internal/app/executor_test.go`'s `fakeExecutor`).
- A provider that cannot support an operation at all (rather than merely
  requiring elevated permissions) simply does not implement that
  interface; callers that need it fail to compile against that adapter
  directly, or - for capability-gated behavior at runtime - see it
  reflected in `Capabilities()`.
