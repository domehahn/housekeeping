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

- Only the document-wide `default: tags:` block is guaranteed to be
  covered - created if missing, appended to if present.
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
- Content reachable only through `include:` (another file, another
  project) is never inspected or modified. `ciyaml.HasIncludes` flags this
  as a warning reason on any matched project, so an operator knows the
  file may have jobs this pass could not see.
- Changes are **always** proposed as a Merge Request - a new branch, one
  commit, one MR per project - never a direct commit to the default
  branch. Nothing in this tool merges that MR; a human always reviews the
  diff and merges it themselves. This is the primary mitigation for the
  inherent risk of programmatically editing CI configuration: the review
  step that would catch a bad patch happens in GitLab's own MR diff view,
  not in this tool's output.
- Revalidation is structural, not a separate check: execution always
  re-fetches the current file and re-runs the patch immediately before
  proposing a change, so a file edited since planning (or a tag already
  merged by a previous run) is handled correctly by construction rather
  than by a bolted-on staleness check.

**Runner tags (`runners` command):**

- GitLab's Runner API replaces a runner's `tag_list` wholesale (it is not
  additive), so this tool always fetches the current list immediately
  before updating and submits the union with the desired tag.
- Every runner report (`runners list`/`evaluate`/`plan`) shows the full
  blast radius: every project using that runner, not just the ones in the
  evaluated scope, split into in-scope and out-of-scope
  (`domain.Runner.OutOfScopeProjectPaths`).
- `execute --apply` refuses to run a plan containing any runner-tag action
  with a non-zero out-of-scope impact unless
  `--confirm-out-of-scope-impact=<N>` is passed, where N must exactly
  equal the plan's total out-of-scope project count. This applies in both
  interactive and non-interactive contexts (unlike `--confirm-scope`,
  which is a non-interactive-only guard) - the risk of affecting an
  unrelated project's pipeline is independent of how the command happens
  to be invoked.

## Consequences

- `pipelines` and `runners` are separate command groups precisely because
  they touch different GitLab resources with different risk profiles and
  different confirmation requirements - collapsing them into one command
  would have hidden that distinction from the operator.
- The known limitation that `include:`-only jobs are not patched is
  permanent, not a "not implemented yet" gap: following `include:` would
  mean fetching and parsing files from other projects/refs this tool may
  not have access to at all, and silently guessing at their content is
  worse than clearly reporting the limitation.
- `SafetyLimits` (`internal/app/safety.go`) was generalized from four
  named fields to a `map[domain.ResourceType]ResourceLimit` specifically
  to accommodate `pipeline_config`/`runner` alongside `project`/`user`
  without repeating the max-actions/max-percentage plumbing a third and
  fourth time; a resource type with no configured limit fails closed
  (`ResourceLimit{0, 0}`, blocking all actions of that type) rather than
  silently allowing unlimited actions.
