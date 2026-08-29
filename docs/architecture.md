# Architecture

## Context

scm-cleaner automates discovery and cleanup of stale resources - projects
and users - on source-code-management platforms. The first and only fully
implemented provider is GitLab (self-managed and GitLab.com). The
architecture is designed so a second provider (GitHub, Bitbucket, Azure
DevOps, Gitea, Forgejo, ...) can be added without changing business logic.

## Goals

- Provider-independent domain model and policies.
- Destructive operations are always: discover -> evaluate -> plan -> review
  -> execute, never a single implicit step.
- Dry run is the default everywhere; `--apply` is required for real effect.
- Unknown data is never treated as a match ("unknown != inactive").
- Every decision is explainable (policy Evaluations carry human-readable
  reasons).
- Safe to run against a large instance: pagination, bounded concurrency,
  context cancellation, retry/backoff for transient errors.

## Non-Goals

- A generic "GitLab API client" - only the operations this tool needs.
- Support for every conceivable GitLab feature (webhooks, CI/CD config,
  merge requests, ...) - out of scope.
- A GUI or a long-running service; this is a CLI meant to be run by an
  operator or from CI/CD.
- Fake/placeholder adapters for other providers "for show" - the second
  provider is added when there is a real need, following the pattern
  described below.

## Layers

```
CLI (internal/cli)
  -> Secret Resolver (internal/secrets)
       -> Environment backend
       -> Native OS keychain backend
  -> Provider Factory (internal/providerfactory; receives generic resolver)
       -> resolved token only
  -> Application / Use Cases (internal/app)
    -> Domain / Policies (internal/domain, internal/policy/*)
      -> Provider Ports (internal/provider)
        -> GitLab Adapter (internal/adapters/gitlab)  [only implemented provider]
        -> (future) GitHub / Bitbucket / ... adapters
```

Dependencies only point downward. `internal/domain` and `internal/policy`
import nothing provider-specific. `internal/app` depends only on the small
interfaces in `internal/provider`, never on `internal/adapters/gitlab`
directly (the only exception is `internal/providerfactory`, whose entire
job is to construct the concrete adapter named by configuration).

### Secret Resolution (`internal/secrets`)

Credentials are represented as a provider-independent `secrets.Reference`
and resolved through an injected `secrets.Resolver`. A central registry
dispatches once on the strongly typed source (`env` or `keychain`). A failure
is returned immediately; there is no cross-source fallback, secret caching, or
global mutable resolver.

The environment backend wraps an injectable lookup function. The keychain
backend wraps `Get`, `Set`, and `Delete` from `zalando/go-keyring`; production
uses macOS Keychain, Linux/BSD Secret Service, or Windows Credential Manager,
while unit tests inject a fake and never access the real store. If account is
omitted, the current OS username is resolved at lookup time.

Normal configuration/provider startup remains read-only. Only explicit
`auth login` and `auth logout` commands write/delete a keychain entry;
`auth status` performs an existence check without displaying the secret.
`auth login` reads from a no-echo terminal prompt and accepts no token flag.

Configuration normalizes both the structured `token` block and legacy
`token_env` into the same reference. `internal/providerfactory` receives the
generic resolver, resolves the reference once, and passes only the resulting
token to the GitLab adapter. Neither the adapter nor application/domain code
knows which secret backend was used. `config validate` uses the same injected
resolver and performs no GitLab network call. See
[ADR 0006](adr/0006-native-secret-resolution.md).

### Domain (`internal/domain`)

Plain, provider-independent types: `Project`, `User`, `Group`, `Scope`,
`Timestamp`/`ActivityStatus`, `PlannedAction`, `Plan`, `Evaluation`, and the
`ProjectPolicy`/`UserPolicy` interfaces. No `*gitlab.Project` or similar
provider SDK type appears here or anywhere outside
`internal/adapters/gitlab`.

`Timestamp` carries an explicit `ActivityKnown`/`ActivityUnknown` status
alongside the `*time.Time` value, so "the provider told us this user has
never signed in" and "the provider could not tell us" are never conflated.
See [ADR 0003](adr/0003-unknown-activity-safe-default.md).

`Clock` is an interface (`RealClock` in production, `FixedClock` in tests)
so every policy is a deterministic, pure function of its inputs - no
`time.Now()` calls scattered through business logic.

### Provider Ports (`internal/provider`)

Small, capability-scoped interfaces rather than one large `Provider`
interface: `ProjectReader`, `ProjectDeleter`, `ProjectArchiver`,
`GroupMemberReader`, `GroupMemberRemover`, `UserBlocker`,
`CurrentUserResolver`, `ScopeResolver`, `CapabilitiesReporter`,
`InfoReporter`, plus `ProjectGetter`/`UserGetter` for single-resource
revalidation. `provider.Client` aggregates all of them purely as a
convenience type for wiring; application functions still take only the
narrow interfaces they actually use as parameters, so a test double only
has to implement what a given use case needs (see
`internal/app/executor_test.go`'s `fakeExecutor` for an example).

`provider.Error` is the generic error taxonomy
(`Authentication`/`Authorization`/`NotFound`/`RateLimit`/`Temporary`/
`Validation`/`Conflict`/`Unknown`) every adapter must translate its
platform's errors into. The application layer and CLI branch on `Kind`,
never on HTTP status codes or SDK-specific error types.

### GitLab Adapter (`internal/adapters/gitlab`)

The only package allowed to import `gitlab.com/gitlab-org/api/client-go`
(the official Go client, successor to `xanzy/go-gitlab`). Responsibilities:

- `client.go` - builds the SDK client, normalizes the base URL, configures
  retry/backoff.
- `groups.go` - resolves a group path to a `domain.Scope`, walks the
  subgroup tree (breadth-first, fully paginated, de-duplicated) when
  `--recursive` is set.
- `projects.go` - lists/deletes/archives/gets projects.
- `users.go` - lists direct group members, optionally enriches with
  admin-only activity fields, removes group members, blocks users.
- `provider.go` - `Capabilities()`/`Info()`.
- `mapper.go` - GitLab SDK type -> domain type conversion. This is the
  *only* place that GitLab JSON field names and semantics are known.
- `errors.go` - GitLab SDK error -> `provider.Error` classification.

See "GitLab API limitations" below for what this adapter does and does not
attempt.

### Policies (`internal/policy/project`, `internal/policy/user`)

Pure functions of `(domain.Project | domain.User, Clock)` producing a
`domain.Evaluation{Match, Reasons}`. No I/O, no provider knowledge.

- `project.InactivePolicy`, `project.ArchivedPolicy`, `project.NamePolicy`,
  `project.Protection`.
- `user.LastLoginPolicy`, `user.LastActivityPolicy`,
  `user.InactiveUserPolicy` (combines the two with `match: all|any`),
  `user.Protection`.

Protection rules are evaluated separately from match policies and always
take precedence - see `app.ProjectEvaluation.Matched()` /
`app.UserEvaluation.Matched()`, which are `Evaluation.Match && !Protected`.

### Application / Use Cases (`internal/app`)

Orchestrates ports + policies for each use case:

- `project_cleanup.go` / `user_cleanup.go` - discovery (`ListProjects`/
  `ListGroupMembers` via the resolved scope) and evaluation (running
  policies + protection against every discovered resource).
- `planner.go` / `plan_file.go` - turns matched, non-protected evaluations
  into a `domain.Plan`, with JSON (de)serialization, a SHA-256 integrity
  hash, version checking, and provider/instance verification.
- `safety.go` - the max-actions / max-percentage guards.
- `executor.go` - carries out a plan's actions: dry-run by default,
  optional pre-execution revalidation, idempotent handling of
  already-gone resources, partial-failure tolerance.

### CLI (`internal/cli`)

Cobra command tree. Responsible for flag parsing, merging configuration
(defaults < file < flags; secret values are resolved from their environment
or keychain reference), building the provider client via
`internal/providerfactory`, rendering output (`internal/output`), writing
the audit log (`internal/audit`), and mapping errors to documented exit
codes. Contains no business logic - every command is "gather inputs, call
into `internal/app`, render the result."

## Planning and Execution Model

```
Discovery -> Evaluation -> Plan -> Review -> Execution
```

1. **Discovery**: list raw resources in a resolved scope (`projects list`,
   `users list`).
2. **Evaluation**: run policies + protection against discovered resources,
   producing `Evaluation`s with reasons (`projects evaluate`,
   `users evaluate`).
3. **Plan**: turn matched, non-protected evaluations into a
   `domain.Plan` of `PlannedAction`s, optionally save to disk
   (`projects plan --output-plan`, `users plan --output-plan`). Safety
   guards run here against the real discovered totals.
4. **Review**: a plan file is a plain, readable JSON document a human (or
   another tool) can inspect before anything happens.
5. **Execution**: `execute <plan-file>` - dry run unless `--apply`; with
   `--apply`, an interactive confirmation phrase (or
   `--non-interactive --confirm-scope=<scope>` in CI) is required first.
   Each action is optionally revalidated against current state
   immediately before being carried out.

See [ADR 0002](adr/0002-plan-before-execute.md).

## Safety Model

- **Dry run by default** - `execute` without `--apply` never mutates
  anything.
- **Explicit apply** - `--apply` is required, and only a plan file (not a
  live query) can be executed.
- **Confirmation** - interactive typed confirmation, or
  `--confirm-scope` in non-interactive contexts.
- **Plan integrity** - a SHA-256 hash over the plan's canonical content
  detects accidental or malicious edits between `plan` and `execute`.
- **Instance/provider verification** - `execute` refuses to run a plan
  whose recorded provider/instance does not match the one it is currently
  configured against.
- **Plan version check** - an unknown/future plan schema version is
  rejected rather than guessed at.
- **Max-actions / max-percentage guards** - `safety.max_actions` and
  `safety.max_percentage` in configuration (with explicit `--max-actions`/
  `--max-percentage` CLI overrides) refuse to plan or execute a run that
  would touch too many, or too large a fraction of, resources.
- **Protected resources** - explicit username/path lists and regexes,
  access-level protection (e.g. always protect owners), and automatic
  protection of the token's own identity (`exclude_current_user`).
- **Unknown activity is never a match by default** - see
  [ADR 0003](adr/0003-unknown-activity-safe-default.md).
- **Cross-instance activity protects users by default** - GitLab's
  activity fields are instance-wide, so a user active anywhere protects
  them everywhere, automatically; an explicit, disabled-by-default
  override exists for license-cost-aware cleanup - see
  [ADR 0004](adr/0004-billable-seat-override.md).
- **Revalidation** - `execution.revalidate: true` (default) re-checks a
  resource, its direct membership role, identity/path, current protection
  rules, and the authenticated caller immediately before acting.
- **Idempotency** - a resource that is already gone (404) is reported as
  `skipped_already_done`, not as a failure.
- **Partial-failure tolerance** - one failing action does not, by default,
  abort the rest of a run (`execution.fail_fast: false`); the process
  still exits with a documented non-zero code (`ExitPartialExecution`) so
  automation notices.

## GitLab Scope Resolution and Membership Semantics

A `domain.Scope` anchors to a group; `Recursive` controls whether
descendant subgroups are included. The GitLab adapter resolves this by:

1. `GET /groups/:id` for the anchor.
2. If recursive, a breadth-first walk of `GET /groups/:id/subgroups`
   (paginated at every level, de-duplicated by ID) to build the full
   `scope.GroupIDs` list.

**Direct vs. inherited membership.** GitLab's
`GET /groups/:id/members` returns only *direct* members of that specific
group - not members who only have access through a parent group. This
adapter deliberately uses that endpoint (not `.../members/all`, which
includes inherited/effective membership) for exactly this reason: a
`RemoveGroupMember` action can only remove a *direct* membership from a
*specific* group, and GitLab has no API to "remove inherited access" at
an arbitrary point in the hierarchy. Each `domain.User` returned by
`ListGroupMembers` therefore carries the ID of the group its direct
membership was found in (`User.GroupID`), and a planned
`remove-from-group` action always targets that group.

If a user is a direct member of more than one group within a recursive
scope (e.g. directly added to both the top-level group and a subgroup),
only the first membership encountered (root group first, then subgroups in
the order they were discovered) is kept for planning purposes. This is a
deliberate simplification, documented in code
(`internal/adapters/gitlab/users.go`) and in the README limitations
section.

## CI Tag Management

Two more resource types exist alongside project/user cleanup:
`pipeline_config` (adding one or more CI tags to a project's `.gitlab-ci.yml`, via a
Merge Request) and `runner` (adding a CI tag to a runner's `tag_list`,
which can affect projects outside the evaluated scope if the runner is
shared). Both use the exact same discover -> evaluate -> plan -> review ->
execute pipeline and the same `domain.Plan`/`PlannedAction` shape as
project/user cleanup - see `internal/app/pipeline_tags.go`,
`internal/app/runner_tags.go`, and the `pipelines`/`runners` CLI commands.

The actual `.gitlab-ci.yml` patch algorithm lives in its own
provider-independent package, `internal/ciyaml`, tested the same way
`internal/policy` is (pure functions, table-driven, no network) even
though the feature as a whole is GitLab-flavored - `.gitlab-ci.yml`'s
shape is not something another provider shares, but the patching logic
itself needs no I/O or GitLab API knowledge, so keeping it a pure package
kept it independently testable and kept the adapter thin (it only handles
fetching the file and opening the Merge Request). See
[ADR 0005](adr/0005-ci-tag-management-scope.md) for the full scope
decisions (what gets patched, what's deliberately left alone, why runner
tag changes require an extra confirmation).

Pipeline selection applies repeatable include/exclude regular expressions to
the full project path, with exclusions taking precedence. Multi-tag actions
are atomic in plan schema version 3; version-2 single-tag plans remain
loadable. A large proposal rollout can be deterministically split by project
path/ID into independently hashed plans. Each batch retains the absolute
safety guard while the unsplit selection is checked against the percentage
guard, so batching is not a safety bypass.

Two read-only provider capabilities complement mutation. The GitLab adapter's
CI-lint call returns `merged_yaml` plus include metadata for effective tag
analysis, while its Merge Request reporter returns proposal states ordered by
latest update. Neither capability changes repository content. Included source
files remain outside the mutation boundary.

The CI parser understands GitLab's optional leading `spec:` header document
and patches only the following configuration document. Runner discovery uses
GitLab's project endpoint as an availability list. The adapter distinguishes
project, group, inherited-group, and instance reach; only reach that can be
proven within the resolved scope is plannable. Execution recomputes this proof
and uses an expected-tag-list preflight comparison before GitLab's whole-list
update. The API has no atomic conditional update, so this is deliberately
documented as a best-effort conflict check rather than a transaction.

## Provider Extension

Adding a provider means:

1. Implement the interfaces in `internal/provider` (or the subset a given
   use case needs) in a new `internal/adapters/<provider>` package,
   mapping that provider's API types to `internal/domain` types.
2. Add one `case` to `internal/providerfactory.New`.
3. Nothing in `internal/app`, `internal/policy`, or the CLI command bodies
   changes - they already depend only on `internal/provider`'s interfaces
   and `internal/domain` types.

See the README's "Adding another Provider" section for a concrete sketch.
