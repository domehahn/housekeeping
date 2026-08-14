# 0003: Unknown activity data is never treated as a match by default

## Status

Accepted

## Context

Not every piece of activity data scm-cleaner would like to have is always
available:

- GitLab's `last_sign_in_at`/`current_sign_in_at`/`last_activity_on` user
  fields are only populated for an instance administrator (or for a user's
  own profile). A token belonging to a group Owner who is not an instance
  admin will not see them for other users at all.
- A permission error partway through discovery could, if handled
  carelessly, leave a resource's fields at their zero value.

A `*time.Time` field that is `nil` is ambiguous: it could mean "known to
have never happened" (a user who has literally never signed in) or "we do
not know" (the provider did not tell us). Treating both cases identically
- as many naive implementations do by just checking `== nil` - means a
missing-permission situation can silently look identical to "this
resource has been inactive forever," which is exactly the input an
inactivity policy is designed to match.

## Decision

Introduce `domain.Timestamp{At *time.Time, Status ActivityStatus}` with
`ActivityStatus` being `known` or `unknown`. `Known(nil)` explicitly means
"known to have never happened"; `Unknown()` means "the provider could not
determine this." Every point where GitLab data is mapped into a
`domain.Timestamp` (`internal/adapters/gitlab/mapper.go`) makes this
determination explicitly and documents why (e.g. project
`last_activity_at` is always `Known` because it requires no special
permission; user `last_sign_in_at`/`last_activity_on` are `Unknown` unless
the token is confirmed to belong to an administrator).

Policies (`user.LastLoginPolicy`, `user.LastActivityPolicy`,
`project.InactivePolicy`) treat `Unknown` as **not a match** by default
(`unknown_activity: skip`). Two additional, explicitly opt-in modes exist:
`warn` (still never matches, but the reason string flags it so operators
notice the data-quality gap) and `match` (treats unknown as satisfying the
threshold - dangerous, and must be set deliberately in configuration; it
is never the default).

`app.UserEvaluation.UnknownActivity` is tracked and reported independently
of whether a match occurred, so `users evaluate`/`plan` output always
surfaces "N users with unknown activity" regardless of `unknown_activity`'s
setting - a permission gap should be visible even when it happens not to
change the outcome.

## Consequences

- A group Owner running scm-cleaner without instance-admin rights will see
  accurate, honest "unknown" activity data instead of a false "everyone is
  inactive" (or the opposite: false "nobody is inactive" if unknown were
  treated as never-matching without being surfaced at all) result.
- Achieving the most powerful cleanup (acting on real login/activity data)
  requires either an admin token or accepting the more limited guarantees
  a non-admin token can provide - `provider capabilities` reports this
  explicitly (`UserLastLogin`/`UserLastActivity` show `requires-admin`).
- `unknown_activity: match` exists for operators who have deliberately
  decided that "we don't know, and that's fine, remove them anyway" is an
  acceptable risk for their situation - but it is never reached by
  accident.
