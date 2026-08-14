# 0002: Plan before execute, dry run by default

## Status

Accepted

## Context

scm-cleaner performs operations - deleting projects, removing group
members, blocking users - that are difficult or impossible to reverse.
Discovering *and* acting on resources in a single command invites two
classes of mistake: acting on stale information gathered moments earlier
in the same process, and acting before a human (or a second automated
check) has had a chance to review exactly what will happen.

## Decision

Split every destructive workflow into five explicit stages: Discovery ->
Evaluation -> Plan -> Review -> Execution.

- `projects/users list` - discovery only.
- `projects/users evaluate` - discovery + policy evaluation, printed for
  review; produces no artifact and performs no mutation.
- `projects/users plan [--output-plan FILE]` - evaluation, safety-guard
  check, and (optionally) serialization of the resulting `domain.Plan` to
  a reviewable JSON file. Still performs no mutation.
- `execute <plan-file>` - reads a previously generated plan. Without
  `--apply`, every action is simulated. With `--apply`, an explicit
  confirmation (interactive typed phrase, or `--non-interactive
  --confirm-scope=<path>` for CI) is additionally required before any
  mutating call is made.

There is no single command that both discovers and destroys.

## Consequences

- An operator (or a CI pipeline) can always inspect exactly what will
  happen - resource IDs, names, actions, and reasons - before anything is
  changed.
- A plan file is a durable artifact that can be reviewed by a second
  person, diffed against a previous run, or replayed later (subject to
  revalidation - see `execution.revalidate`).
- The extra step is deliberate friction. It cannot be bypassed by a single
  flag; `--apply` only ever applies to an already-generated plan, never to
  a live discovery.
- Plans carry enough metadata (provider, instance, scope, a content hash)
  to detect being run against the wrong target or having been tampered
  with - see the Threat Model document.
