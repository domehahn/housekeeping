# scm-cleaner

A safe, provider-independent CLI for discovering and cleaning up stale
projects and users, proposing GitLab CI runner-tag changes through Merge
Requests, and managing tags on eligible GitLab runners.
GitLab (self-managed and GitLab.com) is fully implemented today; the
architecture is designed so GitHub, Bitbucket, Azure DevOps, Gitea, and
Forgejo can be added later without touching business logic.

## 1. Project description

scm-cleaner discovers projects, direct group members, pipeline configuration,
and available runners within a GitLab group (optionally recursive across
subgroups). It evaluates them against configurable policies, produces a
reviewable plan, and executes that plan only after explicit approval. Pipeline
tag changes are proposed as Merge Requests; runner-tag changes use conservative
reach analysis and fail closed when their effect cannot be proven.

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
go install github.com/domehahn/housekeeping/cmd/scm-cleaner@v0.2.1
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
  --token-env GITLAB_TOKEN \
  --group my-company/sandbox \
  provider info

scm-cleaner --gitlab-url https://gitlab.example.com \
  --token-env GITLAB_TOKEN \
  --group my-company/sandbox --recursive \
  projects list
```

## 6. GitLab authentication

scm-cleaner authenticates with a GitLab **personal, project, or group access
token** sent as the `PRIVATE-TOKEN` header. Configuration stores only a
reference to the token. The value is resolved from either an environment
variable or the operating system's native credential store; it is **never**
stored in a config file, CLI flag, plan file, log, or error message. Resolution
uses exactly the selected source and never silently falls back to another one.

Recommended scope: `api` is broadest and sufficient for everything this tool
does; a narrower `read_api` token works for `list`/`evaluate`/`plan` but cannot
execute anything. For production use, create a **dedicated bot account** scoped
to only the groups you intend to clean up. Some data (see
[section 15](#15-last-login-vs-last-activity)) and actions
([section 17](#17-permissions)) require an instance administrator token.

### 6.1 Environment variable

This is the recommended mode for CI. GitLab masked/protected CI variables can
populate `GITLAB_TOKEN` without putting its value into the repository:

```yaml
provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token:
      source: env
      env: GITLAB_TOKEN
```

```bash
export GITLAB_TOKEN="glpat-xxxxxxxxxxxxxxxxxxxx"
scm-cleaner --config scm-cleaner.yaml config validate
```

An unset variable and an explicitly empty variable both fail closed, with
distinct errors. The CLI never tries the keychain as a fallback.

### 6.2 Native OS keychain

This mode is convenient for interactive workstations. `service` is required;
`account` is optional and defaults to the username reported by the operating
system. Set it explicitly for bot credentials and portable configuration.

```yaml
provider:
  type: gitlab
  gitlab:
    base_url: https://gitlab.example.com
    token:
      source: keychain
      service: scm-cleaner
      account: gitlab-bot
```

macOS Keychain (the first command prompts for the token):

```bash
security add-generic-password -U -s "scm-cleaner" -a "gitlab-bot" -w
security find-generic-password -s "scm-cleaner" -a "gitlab-bot" -w
scm-cleaner --config scm-cleaner.yaml config validate
```

Linux/BSD Secret Service (`secret-tool` is supplied by libsecret):

```bash
secret-tool store --label="scm-cleaner GitLab token" service scm-cleaner username gitlab-bot
secret-tool lookup service scm-cleaner username gitlab-bot
scm-cleaner --config scm-cleaner.yaml config validate
```

Windows Credential Manager (PowerShell with the `CredentialManager` module):

```powershell
Install-Module CredentialManager -Scope CurrentUser
$token = Read-Host "GitLab token" -AsSecureString
New-StoredCredential -Target "scm-cleaner:gitlab-bot" -UserName "gitlab-bot" -SecurePassword $token -Type Generic -Persist LocalMachine
Remove-Variable token
scm-cleaner --config scm-cleaner.yaml config validate
```

On Windows the backend identifies entries as `service:account`. Linux/BSD
requires an unlocked Secret Service session and default collection; headless
CI should normally use `source: env`. The lookup commands above print the
credential; use them only for deliberate troubleshooting in a private terminal.

### 6.3 Legacy configuration and CLI override

Existing files remain valid and are normalized internally to an environment
reference:

```yaml
token_env: GITLAB_TOKEN
```

The existing `--token-env GITLAB_TOKEN` flag remains supported and overrides
either configured token source for that invocation. A file containing both
`token:` and `token_env:` is rejected as ambiguous.

## 7. Configuration

YAML configuration, validated strictly (unknown keys are rejected so typos
never fail silently). See [`examples/config.yaml`](examples/config.yaml)
for a complete, commented example and
[`examples/policies.yaml`](examples/policies.yaml) for just the
policy-relevant block.

**Precedence** (lowest to highest): built-in defaults < config file < CLI
flags. A `--group` flag always overrides `scope.group` in the file, for
example. Secret token values are resolved separately through the configured
`token` reference; `SCM_CLEANER_DEBUG` only controls logging.

Validate syntax, semantics, and credential resolution without making a GitLab
network request (a keychain source may prompt according to OS policy):

```bash
scm-cleaner --config myconfig.yaml config validate
```

Authentication configuration parameters:

| Parameter | Required/default | Function | Example |
|---|---|---|---|
| `provider.gitlab.token.source` | Required for structured syntax | Select exactly `env` or `keychain` | `source: keychain` |
| `provider.gitlab.token.env` | Required for `source: env` | Name the variable holding the token | `env: GITLAB_TOKEN` |
| `provider.gitlab.token.service` | Required for `source: keychain` | Native credential-store service name | `service: scm-cleaner` |
| `provider.gitlab.token.account` | Optional for `source: keychain`; current OS user | Native credential-store account | `account: gitlab-bot` |
| `provider.gitlab.token_env` | Legacy alternative | Name an environment variable; cannot coexist with `token` | `token_env: GITLAB_TOKEN` |

Fields belonging to another source are invalid: `env` cannot be combined with
`service`/`account`, and `keychain` cannot be combined with `env`. Unknown
sources, literal token values, unknown YAML fields, missing credentials, and
empty credentials all fail validation.

## 8. Commands

All examples below use `company/platform` as the target group, assume
`scm-cleaner.yaml` contains the GitLab base URL and an environment token reference
as shown in [`examples/config.yaml`](examples/config.yaml), and assume the
token is exported:

```bash
export GITLAB_TOKEN="glpat-xxxxxxxxxxxxxxxxxxxx"
```

Global flags can be supplied before or after a subcommand and are inherited by
all commands:

| Parameter | Required/default | Function | Example |
|---|---|---|---|
| `--config FILE` | Optional; auto-uses `scm-cleaner.yaml` when present | Load strict YAML configuration | `--config production.yaml` |
| `--gitlab-url URL` | Required for live GitLab calls unless configured | Override `provider.gitlab.base_url` | `--gitlab-url https://gitlab.example.com` |
| `--token-env NAME` | Optional compatibility/override flag | Override the configured source with an environment-variable reference; never pass the token itself | `--token-env GITLAB_TOKEN` |
| `--group PATH` | Required for scoped commands unless configured | Select a group or subgroup | `--group company/platform` |
| `--recursive` | Optional; default from config, otherwise `false` | Include every descendant subgroup | `--recursive` |
| `--workers N` | Optional; config/default is `5` | Bound concurrent read operations | `--workers 8` |
| `--output FORMAT` | Optional; `table` | Render `table`, `json`, or `yaml` where supported | `--output json` |
| `--audit-log FILE` | Optional; disabled | Append execution outcomes as owner-readable JSON Lines | `--audit-log audit.jsonl` |
| `--insecure-skip-tls-verify` | Optional; `false` | Disable TLS verification and print a warning | Use only in an explicitly accepted test environment |
| `-h`, `--help` | Optional | Show authoritative help for the selected command | `scm-cleaner runners plan --help` |

### 8.1 Version and shell completion

Show build metadata:

| Command/parameter | Required/default | Function |
|---|---|---|
| `version` | Command | Print version, commit, build date, and Go version |
| `version --output table\|json\|yaml` | Optional; `table` | Select version output format |
| `completion <shell>` | Shell required: `bash`, `zsh`, `fish`, or `powershell` | Generate a completion script on stdout |
| `completion <shell> --no-descriptions` | Optional; `false` | Omit descriptions from generated completions |

```bash
scm-cleaner version
scm-cleaner version --output json
```

Generate completion for the current shell:

```bash
scm-cleaner completion bash > scm-cleaner.bash
scm-cleaner completion zsh > _scm-cleaner
scm-cleaner completion fish > scm-cleaner.fish
scm-cleaner completion powershell > scm-cleaner.ps1
```

### 8.2 Configuration and diagnostics

Validate YAML, unknown fields, semantic constraints, and the configured token
environment variable without modifying GitLab:

| Command | Local parameters | Function |
|---|---|---|
| `config validate` | None | Validate configuration and token-environment presence; performs no GitLab API call |
| `doctor` | None | Run read-only config, connection, authentication, scope, permission, and capability checks |

Both commands inherit the global parameters above. `doctor` needs live provider
configuration; `config validate` does not connect to GitLab.

```bash
scm-cleaner --config scm-cleaner.yaml config validate
```

Run read-only connectivity, authentication, group-resolution, permission, and
capability diagnostics:

```bash
scm-cleaner --config scm-cleaner.yaml doctor

# The same diagnostic using flags instead of a configuration file:
scm-cleaner doctor \
  --gitlab-url https://gitlab.example.com \
  --token-env GITLAB_TOKEN \
  --group company/platform \
  --recursive

# Enable diagnostic logging for one invocation:
SCM_CLEANER_DEBUG=1 scm-cleaner --config scm-cleaner.yaml doctor
```

### 8.3 Provider inspection

`provider list` is static and works without configuration or credentials.
`provider info` and `provider capabilities` query the configured instance.

| Command | Local parameters | Function |
|---|---|---|
| `provider list` | None | List provider types compiled into the binary and required configuration |
| `provider info` | None | Show provider, instance URL/version, authenticated identity, and admin status |
| `provider capabilities` | None | Report support/required privilege for every provider operation |

```bash
# Providers compiled into this binary:
scm-cleaner provider list

# Connected instance, server version, authenticated user, and admin status:
scm-cleaner provider info \
  --gitlab-url https://gitlab.example.com \
  --token-env GITLAB_TOKEN

# Operations available to the current credentials:
scm-cleaner provider capabilities \
  --gitlab-url https://gitlab.example.com \
  --token-env GITLAB_TOKEN
```

### 8.4 Project discovery, evaluation, and plans

`projects list` discovers projects. `projects evaluate` applies inactivity,
archived-state, include/exclude, and protection policies without producing a
plan. `projects plan` converts matches into `report`, `archive`, or `delete`
actions. Exclusions and protection always win over matches.

| Command/parameter | Required/default | Function |
|---|---|---|
| `projects list` | No local parameters | List project ID, path, archived state, and last activity |
| `projects evaluate` | Policy from config or flags | Evaluate and show matched projects/reasons without creating a plan |
| `projects plan` | Policy from config or flags | Evaluate and render a plan; save it when `--output-plan` is set |
| `--inactive-for DURATION` | Optional; config policy otherwise | Match activity older than values such as `90d` |
| `--include REGEX` | Optional, repeatable | Add a project path/slug include expression |
| `--exclude REGEX_OR_PATH` | Optional, repeatable | Add an exclusion; exclusions always win |
| `--action report\|archive\|delete` | Plan only; `report` | Choose the planned action |
| `--output-plan FILE` | Plan only; optional | Save canonical hashed JSON for `execute` |
| `--max-actions N` | Plan only; config limit | Explicitly override `safety.max_actions.projects` for this run |
| `--max-percentage N` | Plan only; config limit/`0` disabled | Override `safety.max_percentage.projects` for this run |

The evaluate/plan parameters augment or override project policies loaded from
configuration. Archived-state and protection rules are configuration-only.

```bash
# List projects directly in the group and every descendant subgroup:
scm-cleaner projects list \
  --group company/platform --recursive

# Find projects inactive for more than 90 days. Include/exclude flags are
# repeatable and augment configuration-file rules:
scm-cleaner projects evaluate \
  --group company/platform --recursive \
  --inactive-for 90d \
  --include 'sandbox|experiment' \
  --exclude '^company/platform/permanent-demo$'

# Report-only plan: records matches but causes no provider mutation:
scm-cleaner projects plan \
  --group company/platform --recursive \
  --inactive-for 90d --action report \
  --output-plan project-report.json

# Archive plan:
scm-cleaner projects plan \
  --group company/platform --recursive \
  --inactive-for 180d --action archive \
  --output-plan project-archive.json

# Delete plan. The explicit override applies only to this planning run:
scm-cleaner projects plan \
  --group company/platform --recursive \
  --inactive-for 365d --action delete \
  --max-actions 5 --max-percentage 10 \
  --output-plan project-delete.json
```

### 8.5 User discovery, evaluation, and plans

`users list` returns unique users discovered through direct memberships in the
resolved scope (retaining the first direct membership found for each user).
`users evaluate` checks last login and last activity. `--match all` requires
both configured criteria to match; `--match any` requires either. `users plan`
supports `report`, `remove-from-group`, and instance-wide `block` actions.

| Command/parameter | Required/default | Function |
|---|---|---|
| `users list` | No local parameters | List user, first direct membership/access level, login, and activity |
| `users evaluate` | Inactivity policy required | Evaluate inactivity/protection without creating a plan |
| `users plan` | Inactivity policy required | Evaluate and render/save a plan |
| `--inactive-for DURATION` | Optional | Set both login and activity thresholds, for example `90d` |
| `--last-login-before DURATION` | Optional; config threshold otherwise | Set/override only the login threshold |
| `--last-activity-before DURATION` | Optional; config threshold otherwise | Set/override only the activity threshold |
| `--match all\|any` | Optional; config/default `all` | Require both criteria or either criterion |
| `--ignore-global-activity-if-non-billable-elsewhere` | Optional; disabled | Enable the billable-seat override described below |
| `--billable-threshold LEVEL` | Optional; `developer` | Set `guest`, `reporter`, `developer`, `maintainer`, or `owner` as the external privileged-membership threshold |
| `--action report\|remove-from-group\|block` | Plan only; `report` | Choose the planned action |
| `--output-plan FILE` | Plan only; optional | Save canonical hashed JSON for `execute` |
| `--max-actions N` | Plan only; config limit | Override `safety.max_actions.users` for this run |
| `--max-percentage N` | Plan only; config limit/`0` disabled | Override `safety.max_percentage.users` for this run |

When shorthand and individual thresholds are combined, the individual
`--last-*-before` value wins for its field. Unknown-activity behavior,
protected usernames/roles, and current-user exclusion are configuration-only.

```bash
# List direct members and their access level/activity data:
scm-cleaner users list \
  --group company/platform --recursive

# Shorthand: both last login and last activity must be older than 90 days:
scm-cleaner users evaluate \
  --group company/platform --recursive \
  --inactive-for 90d --match all

# Independent criteria, matching either one:
scm-cleaner users evaluate \
  --group company/platform --recursive \
  --last-login-before 120d \
  --last-activity-before 60d \
  --match any

# Report-only plan:
scm-cleaner users plan \
  --group company/platform --recursive \
  --inactive-for 90d --action report \
  --output-plan user-report.json

# Remove each matched direct membership from its specific group:
scm-cleaner users plan \
  --group company/platform --recursive \
  --inactive-for 180d --action remove-from-group \
  --output-plan remove-members.json

# Block matched accounts instance-wide (requires an instance administrator):
scm-cleaner users plan \
  --group company/platform --recursive \
  --inactive-for 365d --action block \
  --max-actions 3 --output-plan block-users.json
```

The optional billable-seat override ignores global activity only when a user is
billable in the selected top-level group but has no membership at or above the
chosen threshold elsewhere. It requires Owner on that top-level group and an
instance-administrator token:

```bash
scm-cleaner users evaluate \
  --group company --recursive \
  --inactive-for 90d \
  --ignore-global-activity-if-non-billable-elsewhere \
  --billable-threshold developer
```

### 8.6 Pipeline CI-tag proposals

`pipelines list` reports which projects have `.gitlab-ci.yml`.
`pipelines evaluate` identifies missing tags, parse/fetch errors, protected
projects, and `include:` warnings. `pipelines plan` creates actions that open
one reviewable Merge Request per eligible project; it never merges them.

| Command/parameter | Required/default | Function |
|---|---|---|
| `pipelines list` | No local parameters | Show whether each scoped project has `.gitlab-ci.yml` |
| `pipelines evaluate --tag TAG` | `--tag` required | Analyze current CI YAML and report tag status/reasons |
| `pipelines plan --tag TAG` | `--tag` required | Build Merge Request proposal actions for eligible projects |
| `--output-plan FILE` | Plan only; optional | Save canonical hashed JSON for `execute` |
| `--max-actions N` | Plan only; config limit | Override `safety.max_actions.pipeline_tags` for this run |
| `--max-percentage N` | Plan only; config limit/`0` disabled | Override `safety.max_percentage.pipeline_tags` for this run |

All pipeline commands also use global scope, recursion, workers, provider, and
output parameters. Project protection is loaded from configuration and applies
to pipeline proposals too.

```bash
# Discover CI configuration files:
scm-cleaner pipelines list \
  --group company/platform --recursive

# Check default.tags and existing job-level tags lists:
scm-cleaner pipelines evaluate \
  --group company/platform --recursive \
  --tag k8s-runner

# Create the reviewable proposal plan:
scm-cleaner pipelines plan \
  --group company/platform --recursive \
  --tag k8s-runner \
  --max-actions 10 \
  --output-plan pipeline-tags.json

# Simulate, then open the Merge Requests after confirmation:
scm-cleaner execute pipeline-tags.json
scm-cleaner execute pipeline-tags.json --apply
```

The patch adds the tag to `default.tags` and to jobs/templates that already
define their own `tags:` list. Jobs inheriting `default.tags` remain unchanged,
external includes are reported but not followed, and GitLab `spec:` header
documents are preserved.

### 8.7 Runner-tag management

`runners list` reports runners available to in-scope projects, their type,
tags, explicit out-of-scope assignments, and reach status. `runners evaluate`
checks a desired tag. `runners plan` includes only runners whose effective
reach can be proven safely.

| Command/parameter | Required/default | Function |
|---|---|---|
| `runners list` | No local parameters | List available runners, type, tags, explicit external assignments, and impact status |
| `runners evaluate --tag TAG` | `--tag` required | Report whether each runner has the tag and whether changing it is safe |
| `runners plan --tag TAG` | `--tag` required | Plan updates only for missing-tag runners with provable reach |
| `--output-plan FILE` | Plan only; optional | Save canonical hashed JSON for `execute` |
| `--max-actions N` | Plan only; config limit | Override `safety.max_actions.runner_tags` for this run |
| `--max-percentage N` | Plan only; config limit/`0` disabled | Override `safety.max_percentage.runner_tags` for this run |

Global `--group` and `--recursive` are safety-relevant here: they determine
whether the owning group and all descendant projects are covered.

```bash
# List available project/group/instance runners and impact status:
scm-cleaner runners list \
  --group company/platform --recursive

# Evaluate a tag for one subgroup and all its descendants:
scm-cleaner runners evaluate \
  --group company/platform/subgroup --recursive \
  --tag k8s-runner

# Plan eligible runner updates:
scm-cleaner runners plan \
  --group company/platform/subgroup --recursive \
  --tag k8s-runner \
  --max-actions 5 \
  --output-plan runner-tags.json

# Dry run:
scm-cleaner execute runner-tags.json

# Non-interactive apply with no explicit out-of-scope assignments:
scm-cleaner execute runner-tags.json --apply --non-interactive \
  --confirm-scope company/platform/subgroup

# If the reviewed plan records three explicit out-of-scope assignments:
scm-cleaner execute runner-tags.json --apply --non-interactive \
  --confirm-scope company/platform/subgroup \
  --confirm-out-of-scope-impact 3
```

Project runners are evaluated through explicit assignments. A group runner is
eligible only when its owning group is contained by a recursive scope. Parent-
group runners inherited by a subgroup, instance runners, and unknown types are
shown as `blocked` and omitted from plans.

### 8.8 Executing any saved plan

`execute` accepts plans from all four planners. It validates the plan hash,
schema version, provider, instance, action limits, and current resource state.
Dry run is the default.

| Command/parameter | Required/default | Function |
|---|---|---|
| `execute PLAN_FILE` | Plan file required | Load, validate, simulate, or apply a saved JSON plan |
| `--apply` | Optional; `false` | Perform mutations; without it every action is a dry run |
| `--non-interactive` | Optional; `false` | Disable prompting; with `--apply`, requires `--confirm-scope` |
| `--confirm-scope PATH` | Required for non-interactive apply | Must exactly equal the path stored in the plan |
| `--confirm-out-of-scope-impact N` | Required when runner plan impact is non-zero | Must exactly equal the plan's explicit external-project count |
| `--max-actions N` | Optional; config limit | Override the absolute guard for this execution |
| `--max-percentage N` | Accepted for interface compatibility; no execute-time effect | Percentage needs the discovery total and is enforced during planning only |
| global `--audit-log FILE` | Optional | Append one JSONL record per outcome, including dry-run outcomes |

`execution.revalidate` and `execution.fail_fast` are configuration-only; their
safe defaults are `true` and `false` respectively.

```bash
# Simulate every action:
scm-cleaner execute project-delete.json

# Interactive apply (requires typing "apply N actions"):
scm-cleaner execute project-delete.json --apply

# Non-interactive apply requires an exact scope confirmation:
scm-cleaner execute project-delete.json --apply --non-interactive \
  --confirm-scope company/platform

# Record every apply outcome in an audit log:
scm-cleaner execute project-archive.json --apply --non-interactive \
  --confirm-scope company/platform \
  --audit-log scm-cleaner-audit.jsonl

# Explicitly override the absolute action guard for this execution only:
scm-cleaner execute project-delete.json --apply --non-interactive \
  --confirm-scope company/platform --max-actions 20
```

One failed action does not stop unrelated actions unless
`execution.fail_fast: true`. Already completed actions are idempotent skips.
Live protection and activity are revalidated by default; runner actions also
re-resolve scope/reach and perform a final tag-list conflict check.

### 8.9 Machine-readable output

Commands that return structured results support table, JSON, and YAML output.
Table output contains no ANSI color codes and is safe for pipes and CI logs.

```bash
scm-cleaner projects list --group company/platform --output json > projects.json
scm-cleaner users evaluate --group company/platform --inactive-for 90d --output yaml
scm-cleaner provider capabilities --output table
```

Run `scm-cleaner <command> --help` or
`scm-cleaner <command> <subcommand> --help` for the authoritative flags of the
installed version.

## 9. Dry run

**Dry run is the default for `execute`.** Without `--apply`, every planned
action is simulated - no network mutation is performed - and the output is
clearly labeled `dry_run`. Nothing in this tool ever deletes or modifies a
resource as a side effect of discovery, evaluation, or planning; only
`execute --apply` can.

## 10. Plans

`projects plan`, `users plan`, `pipelines plan`, and `runners plan` evaluate
resources and, with `--output-plan FILE`, write a plan document:

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
- **Runner reach proof** (`runners` only): project-runner assignments are
  enumerated; a group runner is actionable only from its owning group with
  `--recursive`. Inherited ancestor-group runners, instance runners, and
  otherwise unprovable reach are blocked and omitted from plans. Explicit
  assignments outside the scope additionally require
  `--confirm-out-of-scope-impact=<N>` matching the plan total - see
  [§25](#25-runner-tag-cleanup) and
  [ADR 0005](docs/adr/0005-ci-tag-management-scope.md).
- **Plan integrity**: SHA-256 hash + provider/instance verification (see
  above).
- **Revalidation** before each destructive call - pipeline protection and
  the live CI file are checked again; runner scope, project assignments and
  tag lists are checked again. A changed runner reach or concurrent tag edit
  fails rather than overwriting live state.
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
| Update a runner's tags | GitLab's **`manage_runner` permission** for that runner; this is typically Maintainer+ for an eligible project runner or Owner for an owned group runner |

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
- Reads and safely patches `.gitlab-ci.yml`, preserves GitLab `spec:` header
  documents, and opens/reuses content-addressed Merge Request proposals.
- Lists runners available to scoped projects, distinguishes project/group/
  instance reach, and performs best-effort concurrent tag-update detection.
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
       ref, err := cfg.Provider.GitHub.SecretReference()
       if err != nil {
           return nil, err
       }
       token, err := resolver.Resolve(ctx, ref)
       if err != nil {
           return nil, fmt.Errorf("resolve github credential: %w", err)
       }
       ...
       return github.New(github.Options{ Organization: cfg.Provider.GitHub.Organization, Token: token })
   ```

   Normalize that provider's configuration into `secrets.Reference`; do not
   teach the adapter about environment variables, keychains, or resolver
   implementations.

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
  401/403/404/429/500 error classification, retry-safe Merge Request
  proposals, GitLab CI multi-document preservation, runner reach analysis,
  live execution revalidation, and concurrent tag-change detection.
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
GitLab's Runner API and can affect explicit project assignments plus every
project reached implicitly through group or instance availability. This
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
- **GitLab CI header documents are preserved.** Files using a leading
  `spec:` header separated from the CI configuration by `---` are decoded
  and re-encoded as two documents; other multi-document streams fail closed.
- **Regex ReDoS**: not specifically mitigated beyond relying on Go's RE2-
  derived `regexp` package, which has linear-time matching guarantees
  regardless of pattern shape.
- **No remote secret-manager integration yet** (Vault, AWS/Azure Secrets
  Manager, Kubernetes Secrets). Environment variables and the native OS
  keychain are supported. Resolution never falls back between sources.

## 24. Pipeline tag cleanup

Adds a CI tag to the `default: tags:` block of every project's
`.gitlab-ci.yml` in scope (creating the block if missing), and to any job
that already defines its own `tags:` list. A job with no `tags:` of its
own is left alone - it already inherits from `default:`. Changes are
**never** committed directly: `execute --apply` opens one Merge Request
per affected project; nothing merges it automatically.

Proposal branches include a digest of the patched content. Re-running after
a partial failure reuses the matching branch and open Merge Request instead
of becoming permanently stuck on a branch-name conflict.

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

Adds a CI tag directly to the `tag_list` of runners **available to** projects
in scope, via the GitLab Runner API - as opposed to §24, which edits
`.gitlab-ci.yml` files. GitLab's project endpoint also returns inherited group
and instance runners, so availability does not prove actual job usage.

```bash
scm-cleaner runners list --group company --recursive
scm-cleaner runners evaluate --group company --recursive --tag k8s-runner

# Target one subgroup and all projects in its descendant subgroups:
scm-cleaner runners evaluate \
  --group company/platform/subgroup --recursive --tag k8s-runner

scm-cleaner runners plan \
  --group company --recursive --tag k8s-runner \
  --output-plan runner-tags.json

scm-cleaner execute runner-tags.json                 # dry run
scm-cleaner execute runner-tags.json --apply --non-interactive \
  --confirm-scope company \
  --confirm-out-of-scope-impact 3                    # must equal the plan's total exactly
```

For a subgroup scope, project runners explicitly assigned to its projects are
eligible. A group runner owned by that subgroup is eligible only with
`--recursive`, because it is implicitly available to descendant subgroups.
A runner inherited from a parent group is shown as `blocked`; target the
owning parent group with `--recursive` if that wider change is intended.
Instance runners are blocked because a group-scoped query cannot prove their
instance-wide reach.

`--confirm-out-of-scope-impact` is required whenever an eligible runner has
explicit project assignments outside the evaluated scope. `execute` prints
those paths and, immediately before updating, re-resolves the group, projects,
runner reach, and current tags. A changed reach requires a new plan; a
tag edit detected by the final preflight check is not overwritten. GitLab
does not expose an atomic compare-and-swap for the subsequent PUT, so a very
narrow external race remains. See
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
- Optional remote secret resolvers (Vault / AWS / Azure / Kubernetes) behind
  the generic resolver interface.
- Re-evaluating the max-percentage safety guard against a freshly
  discovered total at execute time (currently only enforced at plan time).
