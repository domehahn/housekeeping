# scm-cleaner

A safe, provider-independent CLI for discovering and cleaning up stale
resources - projects and users - on source-code-management platforms.
GitLab (self-managed and GitLab.com) is fully implemented today; the
architecture is designed so GitHub, Bitbucket, Azure DevOps, Gitea, and
Forgejo can be added later without touching business logic.

## 1. Project description

scm-cleaner discovers projects and users within a GitLab group (optionally
recursive across subgroups), evaluates them against configurable policies
(inactivity thresholds, name patterns, archived state, last-login vs.
last-activity), produces a reviewable plan of proposed actions, and -
only when explicitly approved - executes that plan.

## 2. Motivation

Long-lived GitLab instances accumulate abandoned sandbox projects and
accounts that have not signed in for months. Cleaning these up by hand does
not scale, and a careless script risks deleting the wrong thing. scm-cleaner
exists to make this cleanup **routine, reviewable, and safe by default** -
every destructive action is planned before it is ever executed, dry run is
the default, and unknown data is never mistaken for "safe to delete."

## 3. Architecture

See [`docs/architecture.md`](docs/architecture.md) for the full picture.
In short:

```
CLI -> Application/Use Cases -> Domain/Policies -> Provider Ports -> GitLab Adapter
```

Business logic (`internal/app`, `internal/domain`, `internal/policy/*`)
never imports the GitLab SDK; the only package allowed to know about
GitLab is `internal/adapters/gitlab`.

## 4. Installation

```bash
git clone https://github.com/domehahn/housekeeping.git
cd housekeeping
make build          # -> bin/scm-cleaner
```

Or directly from a tagged release, without cloning:

```bash
go install github.com/domehahn/housekeeping/cmd/scm-cleaner@latest
# or pin a specific version:
go install github.com/domehahn/housekeeping/cmd/scm-cleaner@v0.1.0
```

This places a `scm-cleaner` binary in `$(go env GOPATH)/bin` (make sure
that directory is on your `PATH`).

Cross-compiled binaries for linux/darwin/windows (amd64+arm64) via
`make build-all`.

**Note on `scm-cleaner version` output**: both `go install` and a plain
`go build ./...` produce a binary that reports
`dev (commit none, built unknown)` - `pkg/version` is only stamped with a
real version/commit/date when built with the `-ldflags` `make build`
(or `make build-all`) already sets. This is expected, not a broken build;
see the `LDFLAGS` variable in the `Makefile` if you want a manually
built binary to report a specific version too.

## 5. Quick start

```bash
export GITLAB_TOKEN="glpat-xxxxxxxxxxxxxxxxxxxx"

scm-cleaner --gitlab-url https://gitlab.example.com \
  --group my-company/sandbox \
  provider info

scm-cleaner --gitlab-url https://gitlab.example.com \
  --group my-company/sandbox --recursive \
  projects list
```

## 6. GitLab token

scm-cleaner authenticates with a GitLab **personal, project, or group
access token** sent as the `PRIVATE-TOKEN` header. The token is read from
an environment variable named by `provider.gitlab.token_env` (default
example: `GITLAB_TOKEN`) - **never** from a config file, a CLI flag, or a
plan file.

Recommended scope: `api` is broadest and sufficient for everything this
tool does; a narrower `read_api` token works for `list`/`evaluate`/`plan`
but cannot execute anything. For production use, create a **dedicated bot
account** scoped to only the groups you intend to clean up, rather than
using a personal token with instance-wide reach. Some data (see
[section 15](#15-last-login-vs-last-activity)) and some actions
([section 17](#17-permissions)) require the token to belong to an instance
administrator; scm-cleaner degrades gracefully (see
[section 16](#16-unknown-activity)) when it does not.

## 7. Configuration

YAML configuration, validated strictly (unknown keys are rejected so typos
never fail silently). See [`examples/config.yaml`](examples/config.yaml)
for a complete, commented example and
[`examples/policies.yaml`](examples/policies.yaml) for just the
policy-relevant block.

**Precedence** (lowest to highest): built-in defaults < config file < CLI
flags. A `--group` flag always overrides `scope.group` in the file, for
example. Secret token values are resolved separately from the environment
variable named by `token_env`; `SCM_CLEANER_DEBUG` only controls logging.

Validate a configuration without connecting to anything:

```bash
scm-cleaner --config myconfig.yaml config validate
```

## 8. Commands

```
scm-cleaner version
scm-cleaner provider list            # static, no config/credentials needed
scm-cleaner provider info            # live: needs a working connection
scm-cleaner provider capabilities    # live: needs a working connection
scm-cleaner projects list
scm-cleaner projects evaluate
scm-cleaner projects plan
scm-cleaner users list
scm-cleaner users evaluate
scm-cleaner users plan
scm-cleaner pipelines list
scm-cleaner pipelines evaluate
scm-cleaner pipelines plan
scm-cleaner runners list
scm-cleaner runners evaluate
scm-cleaner runners plan
scm-cleaner execute <plan-file>
scm-cleaner config validate
scm-cleaner doctor
```

Every command accepts `--output table|json|yaml` (default `table`).
`table` output never contains ANSI color codes, so it is safe piped
through other tools, redirected to a file, or read in CI logs; `NO_COLOR`
is trivially respected because no color is ever emitted.

**`provider list` vs. `provider info`/`capabilities`**: `provider list` is
purely static - it needs no `base_url`, no token, and makes no network
call, so it works before you've configured anything (useful to check what
this build supports). `provider info` and `provider capabilities`
genuinely need a working connection: they report *live* facts (the
authenticated identity, the connected server's version, what your actual
credentials can do right now) that cannot exist without one, so they
validate the full provider configuration first and fail with an
actionable message (e.g. "set `--gitlab-url` or `provider.gitlab.base_url`")
if it's incomplete.

## 9. Dry run

**Dry run is the default for `execute`.** Without `--apply`, every planned
action is simulated - no network mutation is performed - and the output is
clearly labeled `dry_run`. Nothing in this tool ever deletes or modifies a
resource as a side effect of discovery, evaluation, or planning; only
`execute --apply` can.

## 10. Cleanup plans

`projects plan` / `users plan` evaluate resources and, with
`--output-plan FILE`, write a plan document:

```json
{
  "version": 2,
  "provider": "gitlab",
  "instance": "https://gitlab.example.com",
  "scope": { "type": "group", "id": "123", "path": "company/sandbox", "recursive": true },
  "createdAt": "2026-08-14T09:00:00Z",
  "actions": [
    {
      "resourceType": "project",
      "resourceId": "4711",
      "resourceName": "company/sandbox/old-experiment",
      "action": "delete-project",
      "reason": ["last activity: 132 days ago > threshold 90 days"],
      "evaluatedAt": "2026-08-14T09:00:00Z"
    }
  ],
  "hash": "sha256:..."
}
```

- Resource identity is always a stable ID, never only a name.
- `hash` is a required SHA-256 fingerprint over the plan's canonical
  content, checked on load to detect tampering or accidental edits (see
  [ADR 0002](docs/adr/0002-plan-before-execute.md) and the threat model).
- `version`/`provider`/`instance` are checked by `execute` before anything
  runs.

## 11. Execution

```bash
scm-cleaner execute plan.json                # simulation only
scm-cleaner execute plan.json --apply        # actually performs the actions
```

Interactive `--apply` requires typing a literal confirmation phrase (`apply
N actions`) after being shown the scope, provider, and instance. In a
non-interactive context (`--non-interactive`, or stdin is not a TTY),
`--apply` additionally requires `--confirm-scope=<plan's scope path>` -
an extra, explicit guard against an unattended job applying to the wrong
target:

```bash
scm-cleaner execute plan.json --apply --non-interactive --confirm-scope company/sandbox
```

Each action is (by default, `execution.revalidate: true`) re-checked
immediately before it runs. New activity, renamed resources, changed direct
membership roles, newly matching protection rules, and attempts to modify the
authenticated caller are skipped. Already-gone resources are reported as `skipped_already_done`,
not as failures. One failing action does not abort the rest of the run
unless `execution.fail_fast: true`.

Documented exit codes:

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | general error |
| 2 | invalid configuration |
| 3 | authentication error |
| 4 | authorization error |
| 5 | safety guard triggered |
| 6 | partial execution (some actions failed) |
| 7 | plan validation failure |

## 12. Safety guards

- **Dry run by default**, **explicit `--apply`**, **plan before execute**
  (see [ADR 0002](docs/adr/0002-plan-before-execute.md)).
- **Max actions**: `safety.max_actions.projects` / `.users` /
  `.pipeline_tags` / `.runner_tags` (defaults 10 / 20 / 10 / 5). A plan
  exceeding the configured maximum is refused (exit code 5) unless an
  explicit `--max-actions` override is passed on that specific command. A
  resource type with no configured limit at all fails **closed** (blocks
  every action of that type) rather than allowing unlimited actions.
- **Max percentage**: `safety.max_percentage.projects` / `.users` /
  `.pipeline_tags` / `.runner_tags` (0 = disabled) refuses a plan that
  would touch too large a fraction of the discovered total. Only enforced
  at plan time (a plan file doesn't carry the discovered total needed to
  recheck it at execute time - the max-actions guard *is* re-checked then).
- **Protected resources**: explicit paths/usernames, regex patterns, and
  (for users) protected access levels (e.g. always protect Owners) and
  automatic protection of the token's own identity
  (`exclude_current_user: true`).
- **Out-of-scope impact confirmation** (`runners` only): `execute --apply`
  refuses to touch a shared runner used by projects outside the evaluated
  scope unless `--confirm-out-of-scope-impact=<N>` matches the plan's
  total exactly, in both interactive and non-interactive contexts - see
  [§25](#25-runner-tag-cleanup) and
  [ADR 0005](docs/adr/0005-ci-tag-management-scope.md).
- **Plan integrity**: SHA-256 hash + provider/instance verification (see
  above).
- **Revalidation** before each destructive call - for pipeline/runner tag
  actions this is structural: the live file/tag-list is always re-fetched
  immediately before acting, so a stale plan can never apply a stale diff.
- **Unknown activity is never a match by default** (see below).

## 13. Project cleanup

```bash
scm-cleaner projects plan \
  --group company/sandbox --recursive \
  --inactive-for 90d \
  --exclude company/sandbox/permanent-test \
  --action delete \
  --output-plan projects.json

scm-cleaner execute projects.json           # simulate
scm-cleaner execute projects.json --apply   # actually delete
```

Actions implemented: `report` (default), `delete`, `archive`.

## 14. User cleanup

```bash
scm-cleaner users plan \
  --group company --recursive \
  --last-login-before 30d \
  --last-activity-before 30d \
  --match all \
  --action remove-from-group \
  --output-plan users.json

scm-cleaner execute users.json              # simulate
scm-cleaner execute users.json --apply      # actually remove members
```

Actions implemented: `report` (default), `remove-from-group`, `block`
(requires an instance-administrator token; see below). A global
GitLab user **delete** action is deliberately **not implemented** - see
[section 17](#17-permissions) and [Limitations](#23-limitations).

## 15. Last login vs. last activity

These are tracked and configured **separately**, never conflated:

- **Last login** (`last_login_days` / `--last-login-before`): when the
  user last authenticated (GitLab's `last_sign_in_at`).
- **Last activity** (`last_activity_days` / `--last-activity-before`):
  when the user last did anything GitLab records as activity (GitLab's
  `last_activity_on`).

`match: all` (default) requires **both** to exceed their threshold;
`match: any` requires **either**.

**Both fields are instance-wide, not scoped to the group being cleaned
up.** GitLab does not expose a "last activity within group X" value - only
a single, global `last_sign_in_at`/`last_activity_on` per user account.
This has a useful side effect: a user who is inactive in the top-level
group you are cleaning up, but who has recently signed in or been active
in a *different* top-level group (or any other project/group on the same
instance), will **not** match the inactivity policy and will therefore
not be planned for `remove-from-group` in the group you are cleaning up -
their global activity protects them everywhere, automatically, without
any extra configuration. There is deliberately no per-group activity
signal (e.g. via the Events API) implemented, since the coarser,
instance-wide check is already the safer default; see
[Limitations](#23-limitations).

**Optional override for license cost**: on GitLab.com, each top-level
group pays for its own seats independently, so "active elsewhere"
protection can leave an unused, billable Developer/Maintainer seat in the
group you're cleaning up just because the same user holds a free Guest or
Reporter role in some unrelated group. `--ignore-global-activity-if-non-billable-elsewhere`
(or `users.inactive.ignore_global_activity_if_non_billable_elsewhere` in
config) opts into overriding that protection specifically for users who
are billable in the *target* group (per GitLab's own
`billable_members` API - never guessed) but hold no privileged membership
(configurable via `--billable-threshold` / `billable_access_level_threshold`,
default `developer`) in any other group, per `GET /users/:id/memberships`
(admin-only). **Disabled by default** - it trades the coarser, safer
"active anywhere protects you" guarantee for a more precise,
license-cost-aware one, and requires both Owner on the top-level group
being cleaned and an instance-admin token to actually take effect (see
[ADR 0004](docs/adr/0004-billable-seat-override.md)). Protection rules
(§17-style protected usernames/regex/access-levels,
`exclude_current_user`) are never bypassed by this override. If the
required data cannot be fetched, the override fails safe - it is either
not applied at all (missing billable-members access) or skipped for that
specific user (missing membership data), never silently treated as "no
memberships elsewhere."

## 16. Unknown activity

GitLab only returns `last_sign_in_at`/`last_activity_on` for an instance
administrator (or a user's own profile via `GET /user`). A group Owner
token that is not an instance admin simply does not receive this data for
other members.

scm-cleaner never treats "we don't know" the same as "this happened a long
time ago." `unknown_activity` controls the behavior explicitly:

| Value | Behavior |
|-------|----------|
| `skip` (default) | Unknown data never matches. |
| `warn` | Still never matches, but the reason is flagged so you notice the data gap. |
| `match` | Treats unknown as satisfying the threshold. **Dangerous** - must be set explicitly. |

`users evaluate`/`plan` output always reports how many users had unknown
activity data, independent of this setting, so a permission gap is always
visible. See [ADR 0003](docs/adr/0003-unknown-activity-safe-default.md).

## 17. Permissions

| Operation | Requirement |
|---|---|
| List projects/groups/subgroups | Any token with read access to the group |
| List direct group members | Any token with read access to the group |
| Remove a direct group member | **Owner** on that specific group (a personal access token with `api` scope is sufficient - "Manage group members" is an Owner-only permission for groups, unlike for projects) |
| Delete / archive a project | Typically Owner on that project/namespace |
| Read `last_sign_in_at` / `last_activity_on` for other users | **Instance administrator** |
| Block a user | **Instance administrator** |
| Delete a user account | **Instance administrator** - not implemented in this tool, see below |
| List billable members of a group (`--ignore-global-activity-if-non-billable-elsewhere`) | **Owner on that specific top-level group**; does not work on subgroups |
| List a user's memberships instance-wide (same feature) | **Instance administrator** |
| Open a Merge Request proposing a `.gitlab-ci.yml` tag change | At least **Developer** on the project (to push a branch and open a Merge Request) |
| Update a runner's tags | **Maintainer+** on a project the runner is enabled for, or **Owner** on the runner's owning group for a shared runner |

`scm-cleaner provider capabilities` reports what is actually available
given the current token, and `scm-cleaner doctor` runs a full read-only
diagnostic (config, connectivity, auth, group resolution, permissions)
without ever mutating anything.

## 18. Provider architecture

See [`docs/architecture.md`](docs/architecture.md) and
[ADR 0001](docs/adr/0001-provider-abstraction.md). In short: small,
capability-scoped interfaces in `internal/provider`, implemented per
provider in `internal/adapters/<provider>`, selected at runtime by
`internal/providerfactory` based on `provider.type` in configuration.

## 19. GitLab adapter

`internal/adapters/gitlab` uses the official
[`gitlab.com/gitlab-org/api/client-go`](https://gitlab.com/gitlab-org/api/client-go)
Go client. It:

- Resolves a group path and (optionally) walks its full subgroup tree,
  fully paginated and de-duplicated.
- Lists projects via `GET /groups/:id/projects?include_subgroups=...`.
- Lists **direct** group members via `GET /groups/:id/members` (not
  `.../members/all`, which would include inherited membership that cannot
  safely be "removed" at an arbitrary point in the hierarchy - see
  `docs/architecture.md`).
- Optionally enriches member records with admin-only activity fields via
  `GET /users/:id`, bounded by a worker pool (`performance.workers`).
- Deletes/archives projects, removes group members, blocks users.
- Retries 429/5xx responses with exponential backoff (via the SDK's
  built-in `retryablehttp` transport) and never retries 401/403/404.
- Classifies every error into a small, provider-independent taxonomy
  (`provider.Error{Kind}`) before it leaves the adapter.

## 20. Adding another provider

1. Create `internal/adapters/github` (or whichever provider).
2. Implement the `internal/provider` interfaces that provider can support
   - e.g. `ProjectReader`, `ProjectDeleter`, `GroupMemberReader`, ... -
   mapping GitHub's API types to `internal/domain` types in a `mapper.go`,
   exactly like the GitLab adapter does.
3. Translate GitHub's errors into `provider.Error{Kind: ...}` in an
   `errors.go`.
4. Add one case to `internal/providerfactory.New`:

   ```go
   case "github":
       token, err := config.ResolveToken(cfg.Provider.GitHub.TokenEnv)
       ...
       return github.New(github.Options{ Organization: cfg.Provider.GitHub.Organization, Token: token })
   ```

5. **Do not** change `internal/app`, `internal/policy/*`, or CLI command
   bodies - `InactiveProjectPolicy`, `InactiveUserPolicy`, the planner, and
   the executor are already provider-independent and apply unchanged. This
   is a deliberate architectural acceptance criterion, not an aspiration:
   the GitLab adapter is the only concrete provider implementation in this
   repository today, precisely so that claim stays honest rather than
   being propped up by an unused placeholder GitHub adapter.

## 21. Testing

```bash
make test        # go test ./...
make test-race    # go test -race ./...
make coverage     # coverage.html
make integration-test   # opt-in, see below
```

- **Policy/domain tests** use `domain.FixedClock` for deterministic
  boundary testing (e.g. exactly 30 days vs. 29 vs. 31 days ago).
- **`internal/ciyaml` tests** are pure, no-I/O table-driven tests covering
  the full `.gitlab-ci.yml` patch scope: default-block creation/append,
  per-job tag append vs. jobs left alone, hidden template jobs,
  anchors/aliases, reserved keywords, `include:` detection, and full
  idempotency (a second call is byte-identical to the first).
- **GitLab adapter tests** run entirely against `httptest.Server` - no
  test in `internal/adapters/gitlab` touches the network. They cover
  authentication headers, pagination across multiple pages, subgroup
  recursion and de-duplication, direct-vs-admin-enriched member activity,
  nil/unknown activity mapping, delete/remove operations,
  401/403/404/429/500 error classification, the branch/commit/Merge
  Request sequence for pipeline tag proposals, and runner listing/tag
  updates with blast-radius mapping.
- **Integration tests** (`test/integration/gitlab`) are skipped unless
  `GITLAB_INTEGRATION_TEST=true`, `GITLAB_URL`, and `GITLAB_TOKEN` are all
  set. Destructive scenarios additionally require
  `GITLAB_INTEGRATION_ALLOW_DESTRUCTIVE=true` and are deliberately **not
  implemented** in this suite - see the file for rationale.

### Tagged GitLab runners

The repository pipeline provides an opt-in `.runner-tagged` template. A job
can select a tagged runner without hard-coding the installation-specific tag:

```yaml
my-job:
  extends: .runner-tagged
  script: make test
```

Set `SCM_CLEANER_RUNNER_TAG` as a project or group CI/CD variable (the example
default is `docker`). For multiple required tags, copy the template's `tags`
array into the job and add one entry per tag. These are GitLab CI **job tags**,
which select a compatible runner. A runner's own tag list is administered via
GitLab's Runner API and can affect every project assigned to that runner - this
repository's own pipeline does not mutate runner registrations for itself, but
`scm-cleaner` can do exactly this *for other groups/projects* as a deliberate,
guarded feature (see [§24](#24-pipeline-tag-cleanup) and
[§25](#25-runner-tag-cleanup)).

## 22. Security considerations

See [`docs/threat-model.md`](docs/threat-model.md) for the full analysis.
Highlights:

- Tokens are read only from environment variables, never logged, never
  written to config or plan files.
- TLS verification is on by default;
  `provider.gitlab.insecure_skip_tls_verify` (or `--insecure-skip-tls-verify`)
  must be set explicitly and prints a loud warning when used.
- Plan files carry a tamper-detection hash and are validated (version,
  required fields, resource IDs) before use - see
  [ADR 0002](docs/adr/0002-plan-before-execute.md).
- Config parsing rejects unknown fields so a typo in a safety-relevant
  setting fails loudly instead of being silently ignored.

## 23. Limitations

- **GitLab only** in this release; other providers are architecturally
  supported but not implemented (see [section 20](#20-adding-another-provider)).
- **User `last_sign_in_at`/`last_activity_on` require an instance-admin
  token** for anyone other than the token's own owner - a group-Owner-only
  token will see this data as `unknown`, not as "never active" (by
  design, see [ADR 0003](docs/adr/0003-unknown-activity-safe-default.md)).
- **`remove-from-group` only removes direct membership** in the specific
  group where a user was found to be a direct member. If a user is a
  direct member of more than one group within a recursive scope, only the
  first membership encountered (root group first, then subgroups in
  discovery order) is used for planning purposes - see
  `internal/adapters/gitlab/users.go`.
- **The billable-seat override approximates billing for *other* groups.**
  It uses GitLab's own authoritative `billable_members` API for the group
  being cleaned up, but for other groups it can only approximate
  "billable" via a configurable access-level threshold, because GitLab's
  actual rule is subscription-tier-dependent and not queryable for groups
  the token does not own - see
  [ADR 0004](docs/adr/0004-billable-seat-override.md). It also only works
  against a genuine top-level group (the billing API rejects subgroups)
  and requires both Owner (on that group) and instance-admin (for
  cross-instance memberships) simultaneously.
- **No global GitLab user deletion.** `DeleteUser` requires
  instance-administrator rights and is unusually destructive (it can
  affect resources far outside the scope being cleaned up); this tool
  intentionally does not implement it. `remove-from-group` and `block`
  cover the two implemented, well-scoped user actions.
- **Plan-time percentage guard is not re-evaluated at execute time**
  against a fresh discovered total (a plan file does not carry that
  count); the absolute max-actions guard *is* re-checked at execute time.
- **Pipeline tag patching only covers `default: tags:` and jobs that
  already define their own `tags:` list.** A job with no `tags:` of its
  own is left alone by design, and jobs defined only via `include:` from
  another file/project are never inspected - see
  [ADR 0005](docs/adr/0005-ci-tag-management-scope.md).
- **Regex ReDoS**: not specifically mitigated beyond relying on Go's RE2-
  derived `regexp` package, which has linear-time matching guarantees
  regardless of pattern shape.
- **No secret-manager integration yet** (Vault, AWS/Azure Secrets
  Manager, Kubernetes Secrets) - `token_env` (a plain environment
  variable) is the only supported source today; the provider factory is
  structured so a secret-resolution abstraction can be added without
  changing call sites.

## 24. Pipeline tag cleanup

Adds a CI tag to the `default: tags:` block of every project's
`.gitlab-ci.yml` in scope (creating the block if missing), and to any job
that already defines its own `tags:` list. A job with no `tags:` of its
own is left alone - it already inherits from `default:`. Changes are
**never** committed directly: `execute --apply` opens one Merge Request
per affected project; nothing merges it automatically.

```bash
scm-cleaner pipelines evaluate --group company --recursive --tag k8s-runner

scm-cleaner pipelines plan \
  --group company --recursive --tag k8s-runner \
  --output-plan pipeline-tags.json

scm-cleaner execute pipeline-tags.json               # dry run
scm-cleaner execute pipeline-tags.json --apply \
  --non-interactive --confirm-scope company          # opens the Merge Requests
```

Jobs defined only via `include:` (another file or project) are not
covered - `pipelines evaluate`/`plan` flags this per project as a warning
reason rather than silently missing them. See
[ADR 0005](docs/adr/0005-ci-tag-management-scope.md).

## 25. Runner tag cleanup

Adds a CI tag directly to the `tag_list` of runners used by projects in
scope, via the GitLab Runner API - as opposed to §24, which edits
`.gitlab-ci.yml` files. If a runner is **shared**, this can affect
projects *outside* the scope you evaluated. Every report shows that blast
radius explicitly:

```bash
scm-cleaner runners list --group company --recursive
scm-cleaner runners evaluate --group company --recursive --tag k8s-runner

scm-cleaner runners plan \
  --group company --recursive --tag k8s-runner \
  --output-plan runner-tags.json

scm-cleaner execute runner-tags.json                 # dry run
scm-cleaner execute runner-tags.json --apply --non-interactive \
  --confirm-scope company \
  --confirm-out-of-scope-impact 3                    # must equal the plan's total exactly
```

`--confirm-out-of-scope-impact` is required (in both interactive and
non-interactive contexts) whenever a plan touches a shared runner used
outside the evaluated scope; `execute` prints every affected out-of-scope
project path so you can actually look at them before confirming. See
[ADR 0005](docs/adr/0005-ci-tag-management-scope.md).

## 26. Roadmap

- GitHub adapter (organizations, repositories, members) as the second
  concrete provider, validating the abstraction with a real second
  implementation.
- Additional user policies mentioned in the design but not yet
  implemented: `ExternalUserPolicy`, `BlockedUserPolicy`,
  `NoProjectMembershipPolicy`, `ExpiredAccountPolicy`, `BotAccountPolicy`,
  `ServiceAccountPolicy` - all of which fit the existing
  `domain.UserPolicy` interface without further architectural change.
- Secret resolution abstraction (Vault / AWS / Azure / Kubernetes) behind
  the existing `token_env`-shaped configuration.
- Re-evaluating the max-percentage safety guard against a freshly
  discovered total at execute time (currently only enforced at plan time).
