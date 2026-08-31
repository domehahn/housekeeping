# 0005: CI tag management scope (pipeline tags vs. runner tags)

## Status

Accepted

## Context

Beyond removing stale projects/users, an operator may need to roll a new
CI runner tag out across every project in a group. GitLab has two
independent places a tag lives:

1. **Job tags** - the `tags:` GitLab reads from a project's
   `.gitlab-ci.yml` to select a runner for each job.
2. **Runner tags** - the `tag_list` on the runner object itself, via the
   Runner API.

Both are legitimate things to want to change in bulk, and both carry real
risk: editing `.gitlab-ci.yml` files across many repositories can silently
break pipelines if done carelessly, and runners are frequently **shared**
across projects/groups, so changing one can affect repositories the
operator never intended to touch.

## Decisions

**Pipeline tags (`pipelines` command, `internal/ciyaml`):**

- One or more tags can be requested in one operation. The document-wide
  `default: tags:` block is guaranteed to be covered - created if missing,
  appended to if present - and existing values are never overwritten.
- A job that already defines its **own** `tags:` list is also patched (in
  addition to `default:`), so a job overriding the default still receives
  the new tag. A job with **no** `tags:` of its own is deliberately left
  alone - it already inherits from `default:`, and inventing a key for it
  would change how GitLab schedules that job (e.g. interaction with
  `run_untagged`) in a way nothing asked for.
- Hidden (`.`-prefixed) template jobs are treated the same as regular
  jobs, since GitLab's `extends:` merges a template's keys - including
  `tags:` - into whatever job extends it at pipeline-compile time. This
  means patching a template correctly reaches every job that extends it,
  without this tool needing to understand `extends:` itself.
- Content reachable only through `include:` (another file, project, template,
  or remote URL) is never modified. `ciyaml.HasIncludes` flags it during
  root-file evaluation. The separate read-only `pipelines analyze` command
  asks GitLab's CI lint API for `merged_yaml` and include metadata, allowing
  the operator to inspect effective tag coverage without treating an included
  source as a writable target.
- GitLab's two-document component form is supported: a leading `spec:`
  header and the configuration after `---` are decoded separately, and only
  the configuration document is patched. Any other multi-document shape is
  rejected rather than risking silent truncation.
- Changes are **always** proposed as a Merge Request - a content-addressed
  branch and one MR per project - never a direct commit to the default
  branch. Nothing in this tool merges that MR; a human always reviews the
  diff and merges it themselves. This is the primary mitigation for the
  inherent risk of programmatically editing CI configuration: the review
  step that would catch a bad patch happens in GitLab's own MR diff view,
  not in this tool's output.
- The branch includes a digest of the patched content. Existing matching
  branch content and an open MR are reused, so retries after partial failure
  are idempotent instead of conflicting forever.
- Revalidation is structural, not a separate check: execution always
  re-fetches the current file and re-runs the patch immediately before
  proposing a change, so a file edited since planning (or a tag already
  merged by a previous run) is handled correctly by construction rather
  than by a bolted-on staleness check.
- Project identity/path and current protection rules are also re-evaluated
  immediately before the proposal, so protection changes after planning win.
- Repeatable include/exclude expressions select project full paths, with
  exclusions taking precedence. Multi-tag actions use plan schema version 3;
  existing version-2 single-tag plans remain executable.
- Optional deterministic batching sorts by project path/ID and writes ordinary
  independently hashed plan files. Every batch must obey `max-actions`, and
  the complete filtered selection must obey `max-percentage`.
- `pipelines proposals status` reports the most recently updated Merge Request
  whose deterministic branch prefix matches the exact normalized tag set. It
  is status reporting only; scm-cleaner never merges the proposal.
- `--replace-tag OLD:NEW` corrects a wrong tag already rolled out (the
  motivating case: a case typo like `AKS` vs `aks` - GitLab tags have no
  case-folding, so the two are entirely distinct to the scheduler).
  `ciyaml.ReplaceTag`/`ReplaceTags` are deliberately **narrower** than
  `AddTag`/`AddTags`: they only ever touch a `default.tags` or job `tags:`
  list that **already contains** the old tag, removing it and adding the
  new one if not already present - never creating a `default:` block,
  never touching a job that never had the old tag. A rename's diff is
  exactly "the places that had the mistake," nothing broader. `--tag` and
  `--replace-tag` are mutually exclusive on the same invocation, so a
  plan's diff and Merge Request are never ambiguous about which mode
  produced them.
- Opening the corrected Merge Request additionally, best-effort, closes
  any still-open scm-cleaner proposal that proposed one of the old tags -
  identified via the same cryptographic tag-set marker
  `ListPipelineTagProposals` already uses for status reporting, so only
  scm-cleaner's own matching proposals are ever touched, never an
  unrelated Merge Request. Closing (not deleting) is reversible via
  reopening in GitLab, only an `opened`-state proposal is ever closed, and
  a failure to close is never fatal to the rename itself - the corrected
  MR still opens, and the caller is told which old proposal(s), if any,
  could not be closed.

**Runner tags (`runners` command):**

- GitLab's Runner API replaces a runner's `tag_list` wholesale (it is not
  additive), so this tool fetches the current list, computes the union, then
  performs a final expected-list comparison immediately before updating. A
  tag change detected by that preflight comparison returns a conflict. GitLab
  exposes no atomic compare-and-swap for the following whole-list PUT, leaving
  a narrow external race that the adapter cannot eliminate.
- GitLab's per-project runner endpoint lists all runners **available** to a
  project, including inherited group and instance runners. It is not treated
  as proof that jobs actually used a runner.
- `RunnerDetails.Projects` is used only for explicit project assignments,
  split into in-scope and out-of-scope paths. It is not claimed to enumerate
  implicit group or instance reach.
- A project runner is plannable when it remains explicitly assigned to an
  in-scope project. A group runner is plannable only when its owning group is
  inside a recursively resolved scope. An inherited ancestor-group runner,
  instance runner, or unknown type fails closed and is shown as `blocked`.
- `execute --apply` refuses to run a plan containing any runner-tag action
  with a non-zero out-of-scope impact unless
  `--confirm-out-of-scope-impact=<N>` is passed, where N must exactly
  equal the plan's total out-of-scope project count. This applies in both
  interactive and non-interactive contexts (unlike `--confirm-scope`,
  which is a non-interactive-only guard) - the risk of affecting an
  unrelated project's pipeline is independent of how the command happens
  to be invoked.
- Immediately before mutation, execution re-resolves the group and project
  set, re-fetches runner details, and compares live out-of-scope paths to the
  confirmed plan. Any difference requires a newly reviewed plan.
- `--replace-tag OLD:NEW` reuses the exact same compare-and-swap
  (`UpdateRunnerTags` already replaces the whole list, so no new runner
  provider port was needed) and the exact same out-of-scope-impact guard
  as adding a tag. The desired list is computed per rename pair: a rename
  whose old tag is not present on a given runner is left entirely alone
  (mirroring `ciyaml.ReplaceTags`' "only touch what had the old tag"
  contract), so a runner never spuriously gains the new tag without ever
  having carried the old one.

## Consequences

- `pipelines` and `runners` are separate command groups precisely because
  they touch different GitLab resources with different risk profiles and
  different confirmation requirements - collapsing them into one command
  would have hidden that distinction from the operator.
- The limitation that `include:`-only jobs are not patched is intentional:
  following an include for mutation would
  mean fetching and parsing files from other projects/refs this tool may
  not own or have permission to update. Effective read-only analysis supplies
  visibility without silently expanding the write scope.
- `SafetyLimits` (`internal/app/safety.go`) was generalized from four
  named fields to a `map[domain.ResourceType]ResourceLimit` specifically
  to accommodate `pipeline_config`/`runner` alongside `project`/`user`
  without repeating the max-actions/max-percentage plumbing a third and
  fourth time; a resource type with no configured limit fails closed
  (`ResourceLimit{0, 0}`, blocking all actions of that type) rather than
  silently allowing unlimited actions.
