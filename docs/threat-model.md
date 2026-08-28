# Threat Model

scm-cleaner is granted credentials that can delete repositories and modify
or remove user accounts on a real, often production, SCM instance. This
document lists the risks that matter most and the mitigations already in
place. It is not exhaustive; treat it as a living document.

## Assets

- The GitLab access token (and, transitively, everything it can reach).
- Projects and users on the target GitLab instance.
- Plan files, which describe (but do not themselves authorize) destructive
  actions.
- The audit log.

## Threats and Mitigations

### 1. Stolen or leaked API token

**Risk**: the token is exfiltrated (logs, shell history, CI variable leak)
and used to run destructive operations, or simply grants an attacker
whatever access the token has.

**Mitigations**:
- The token is only ever read from an environment variable named by
  `token_env`; it is never written to a config file, a plan file, a log
  line, or an error message (`internal/adapters/gitlab/errors.go`'s
  `safeMessage` deliberately extracts only the message/status, never
  headers or raw request data).
- README documents least-privilege scope recommendations (`api` scope is
  broad; a dedicated bot account restricted to the minimum role needed for
  the target group is recommended).
- Not mitigated by this tool: token storage/rotation - that is the
  operator's responsibility (see "Roadmap" in the README for planned
  Vault/Secrets Manager integration).

### 2. Wrong scope targeted

**Risk**: an operator runs a cleanup against the wrong group (e.g. typo in
`--group`, or a shared config file meant for a different environment).

**Mitigations**:
- `--group` / `scope.group` must resolve to a real GitLab group before
  anything else happens; a typo fails fast with `NOT FOUND`.
- Plans record the exact scope (`type`, `id`, `path`, `recursive`) they
  were generated against.
- Interactive `execute --apply` confirmation shows scope, provider, and
  instance before prompting.
- `--non-interactive --apply` additionally requires `--confirm-scope`
  matching the plan's scope path exactly - a copy-pasted or templated CI
  job cannot silently apply to a different scope than the one the operator
  reviewed.

### 3. Wrong instance targeted

**Risk**: a plan generated against a staging instance is (accidentally or
via a copied CI job) executed against production, or vice versa.

**Mitigations**:
- `Plan.Provider` and `Plan.Instance` are recorded at plan time.
- `execute` fetches the *current* provider's `Info()` and refuses to run
  if it does not match (`app.VerifyAgainstInstance`).

### 4. Manipulated plan file

**Risk**: a plan file is edited (by hand, by a compromised CI step, or by
an attacker with write access to wherever the plan is stored) between
`plan` and `execute` to add or change targets.

**Mitigations**:
- Every saved plan must carry a SHA-256 hash over its canonical content;
  `LoadPlan` rejects a missing hash, recomputes and compares a present one,
  and refuses to execute a plan whose
  content does not match its recorded hash.
- This is a **tamper/accidental-change detector, not a cryptographic
  signature** - anyone who can edit the plan file can also recompute a
  matching hash. It protects against silent corruption and careless
  editing, not a determined attacker with file write access. Treat plan
  files with the same access control as you would treat the token itself
  while they exist on disk.
- Resource identity in a plan is always a stable provider ID, never only a
  name (`internal/domain/action.go`), so even a successfully-edited
  `resourceName` cannot redirect an action to a different resource.

### 5. Overly broad regex in include/exclude/protection rules

**Risk**: a regex intended to match a handful of sandbox projects instead
matches (or fails to exclude) something important, e.g. `.*` used
carelessly, or an anchor omitted.

**Mitigations**:
- Excludes always take precedence over includes (`project.NamePolicy`).
- `config validate` compiles every regex up front and rejects invalid or
  excessively long (>512 char) patterns before any evaluation runs.
- Every match carries an explicit "Reasons" trail
  (`internal/domain/policy.go`'s `Evaluation.Reasons`) so an operator can
  see *why* a given resource matched before planning, let alone executing.
- Not fully mitigated: this project does not attempt to detect
  catastrophic-backtracking (ReDoS) regexes. Go's `regexp` package uses an
  RE2-derived engine with linear-time guarantees regardless of pattern
  shape, which structurally avoids the classic backtracking-based ReDoS
  class entirely.

### 6. Missing or incomplete API data misread as "safe to act on"

**Risk**: a permission gap, an unsupported GitLab edition, or a partial
API failure is misinterpreted as "this user/project is inactive" or "there
is nothing to clean up."

**Mitigations**:
- `domain.Timestamp` carries an explicit `ActivityKnown`/`ActivityUnknown`
  status; unknown data never defaults to a match (`unknown_activity: skip`
  by default) - see [ADR 0003](adr/0003-unknown-activity-safe-default.md).
- A failure resolving whether the caller is an admin, or fetching a given
  user's admin-only fields, degrades that specific piece of data to
  Unknown rather than aborting discovery or silently returning zero
  results (`internal/adapters/gitlab/users.go`).
- A provider/authorization error surfaces as a distinct, documented exit
  code (3/4) rather than being swallowed into "0 resources found."

### 7. Race condition between plan and execute

**Risk**: a resource's state changes for the better (a user logs back in,
a project gets active development) between planning and execution.

**Mitigations**:
- `execution.revalidate: true` (default) re-fetches each resource and direct
  membership immediately before acting. It skips (`skipped_revalidate`) on
  new activity, identity/path changes, membership-role changes, current
  protection-rule matches, or when the target is the authenticated caller.
- If revalidation itself fails (e.g. a transient error), the action is
  skipped, not carried out - "safety over convenience": an action is never
  performed on unconfirmed information.

### 8. Mass deletion / runaway cleanup

**Risk**: a misconfigured threshold (e.g. `days: 0`) or a policy bug
matches far more resources than intended.

**Mitigations**:
- `safety.max_actions` (absolute) and `safety.max_percentage` (relative to
  the discovered total) guards refuse to plan or execute past configured
  limits; overriding either requires an explicit CLI flag.
- Dry run is the default; nothing is destroyed by planning alone.
- Interactive confirmation requires typing the literal action count
  (`apply N actions`), forcing the operator to actually look at the
  number before confirming.

### 9. CI/CD misconfiguration

**Risk**: an automated pipeline runs `execute --apply` unintentionally
(e.g. a variable meant to gate `--apply` is misconfigured, or a shared
pipeline template is reused for a different environment without review).

**Mitigations**:
- `--apply` is never implied by any other flag or by a config file value -
  it must be passed explicitly on that specific invocation.
- Non-interactive apply additionally requires `--confirm-scope` matching
  the plan file, so a template reused across environments without
  updating the scope will fail loudly instead of silently applying to the
  wrong one.
- Audit log (`--audit-log`) records every attempted action and its result,
  independent of the general application log, for post-incident review.

### 10. Secrets in logs

**Risk**: the token or other sensitive values end up in structured logs or
error output.

**Mitigations**:
- The general application log (`log/slog`) never receives the token as a
  field.
- `provider.Error` messages are built exclusively from the response
  message/status (`safeMessage`), never from request headers.
- Audit log records (`internal/audit/record.go`) contain only resource
  identifiers, action, result, and scope - never credentials.

### 11. A proposed CI configuration change breaks a pipeline

**Risk**: an automated `.gitlab-ci.yml` edit (adding a CI tag) is
malformed, or interacts badly with a project's actual pipeline semantics
(e.g. `extends:`, includes, anchors), breaking CI for that project.

**Mitigations**:
- The patch (`internal/ciyaml`) never commits directly - it always opens a
  Merge Request. Nothing in this tool merges it; a human reviews the real
  diff in GitLab's own MR view before it takes effect.
- The patch logic only ever adds a tag to a `tags:` list; it never removes
  or reorders existing content, and is proven idempotent and
  comment/formatting-preserving by its own unit tests
  (`internal/ciyaml/patch_test.go`).
- A document that fails to parse, or whose `default`/job `tags:` value is
  not a list, is reported as a `parse_error` and skipped - never patched
  on a best-effort guess.
- GitLab's supported two-document `spec:` header form is preserved. Any
  unrecognized multi-document stream is rejected instead of truncating it.
- Jobs reachable only through `include:` are never touched; this is
  reported as an explicit warning rather than silently missed.
- Project protection rules and project identity/path are checked again at
  execution time. Proposal branches are content-addressed and an existing
  open MR is reused, making retries after partial failure idempotent.

### 12. A shared runner tag change affects unrelated projects

**Risk**: changing a runner's `tag_list` to route new work to it also
changes routing for every other project/group already using that runner,
if it is shared.

**Mitigations**:
- GitLab's project-runner endpoint is treated as an availability list, not
  evidence that a runner actually ran jobs for a project.
- Explicit project-runner assignments are split into in-scope and
  out-of-scope paths. Implicit reach is handled by runner type: group runners
  require their owning group to be covered recursively; inherited ancestor
  group runners and instance runners fail closed and are omitted from plans.
- `execute --apply` refuses to run a plan touching a shared runner used
  outside the evaluated scope unless
  `--confirm-out-of-scope-impact=<N>` is passed and matches the plan's
  total out-of-scope project count exactly, in both interactive and
  non-interactive contexts. See
  [ADR 0005](adr/0005-ci-tag-management-scope.md).
- Execution re-resolves the scope and projects and recomputes runner reach;
  any changed out-of-scope assignment set requires a new plan.
- The Runner API replaces `tag_list` wholesale. The adapter performs a final
  compare immediately before PUT and returns a conflict if another actor
  changed the tags before that check. GitLab provides no atomic conditional
  update, so an external change in the narrow GET-to-PUT interval remains a
  residual risk and must be monitored through GitLab/audit history.

## Explicitly Out of Scope (for now)

- Protecting against a fully compromised operator workstation (keylogging,
  local token theft) - outside what a CLI tool can defend against.
- Detecting a malicious GitLab instance impersonation (e.g. DNS spoofing)
  beyond standard TLS certificate verification, which is on by default and
  only disabled via the explicit, loudly-warned `--insecure-skip-tls-verify`
  flag.
- Multi-tenant credential isolation - one process, one token, one run.
