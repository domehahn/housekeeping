# 0004: Billable-seat override for cross-group activity protection

## Status

Accepted

## Context

[ADR 0003](0003-unknown-activity-safe-default.md) and the surrounding
design use GitLab's instance-wide `last_sign_in_at`/`last_activity_on`
fields to protect users from removal: a user active anywhere on the
instance is not matched by the inactivity policy for a specific group,
because GitLab exposes no per-group activity signal at all.

This is deliberately the safe default, but it has a cost: on GitLab.com,
each top-level group has its own, independently paid seat count. A user
who holds a costly Developer/Maintainer/Owner membership in the group
being cleaned up, but who is only a free-or-cheap Guest/Reporter in some
unrelated other top-level group, still keeps their expensive membership
in the group being cleaned - because that unrelated group's activity
protects them globally, even though it has nothing to do with whether the
membership being evaluated is actually needed or used.

## Decision

Add an **opt-in** override, `policy/user.BillableSeatOverride`
(`users.inactive.ignore_global_activity_if_non_billable_elsewhere` /
`--ignore-global-activity-if-non-billable-elsewhere`), applied after the
normal evaluation in `app.ApplyBillableSeatOverride`:

A user who did **not** match the standard inactivity policy (i.e. was
protected by recent global activity) is re-matched anyway if:

1. GitLab's own authoritative `GET /groups/:id/billable_members` for the
   group being cleaned up confirms the user counts as billable there
   (never approximated from access level - that determination is
   tier-dependent, see the endpoint's docs), **and**
2. The user holds no group membership at or above a configurable access
   level (`users.inactive.billable_access_level_threshold`, default
   Developer) in any *other* group on the instance, determined via
   `GET /users/:id/memberships` (admin-only).

Protection rules (`users.protection.*`, `exclude_current_user`) are
checked before this override and are never bypassed by it - billing
considerations never override an explicit protection rule.

Both underlying calls fail safe: if the target group's billable-member
list cannot be retrieved (e.g. the token is not Owner on that specific
top-level group), the override is not applied to *anyone* and the error is
surfaced. If a specific user's cross-instance memberships cannot be
retrieved, only that user is skipped (left exactly as the base evaluation
determined) and a warning is emitted - never silently treated as "no
memberships elsewhere" for that user, which would have widened who gets
matched based on missing data.

## Consequences

- This is the one place in the tool's policy layer where "the user is
  billable in group B, but has no privileged role in any other group" is
  approximated by an access-level threshold rather than GitLab's own
  billing determination - because GitLab's billing rule is genuinely
  tier-dependent (e.g. Guest is billable on Premium, not on Free/Ultimate)
  and this adapter has no reliable way to query another group's tier
  without Owner access there. This approximation, its default threshold,
  and its fail-safe behavior on missing data are documented in
  `policy/user.BillableSeatOverride`'s doc comment and the README.
- The feature requires two elevated permission levels at once: Owner on
  the top-level group being cleaned (for `billable_members`) and instance
  administrator (for `/users/:id/memberships`). `doctor` and
  `provider capabilities` report both requirements explicitly so an
  operator can tell up front whether the override can actually run with
  their current token.
- Disabled by default, consistent with "safety over convenience": turning
  this on is a deliberate choice to trade the coarser, safer
  "active-anywhere protects you" guarantee for a more precise,
  license-cost-aware one.
