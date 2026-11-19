# Security Policy

## Supported Versions

Only the latest release and the current `main` branch receive security patches.

| Version | Supported          |
| ------- | ------------------ |
| v0.7.x  | :white_check_mark: |
| < v0.7  | :x:                |
| main    | :white_check_mark: |

## Security Architecture & Threat Model

`scm-cleaner` operates with high-privilege credentials capable of deleting projects and modifying memberships. The project enforces strict security boundaries by design:

- **Zero-Token-Leak Guarantee:** Access tokens are never logged, never saved in plan files, never embedded in error messages, and never stored in plain-text config files.
- **Strict Credential Isolation:** Token resolution uses exclusively the explicitly selected backend (`--token-env` or native OS keychain via `auth login`) with no fallback across trust boundaries.
- **Fail-Closed Safety:** Missing, ambiguous, or incomplete provider metadata is treated as `domain.Unknown()` and never presumed inactive or safe to delete.
- **Two-Phase Commit:** All destructive actions require an explicit `evaluate -> plan -> review -> execute` lifecycle.

For full architectural details, review the [Threat Model Documentation](docs/threat-model.md).

## Reporting a Vulnerability

If you discover a potential security vulnerability in `scm-cleaner`:

1. **Do not open a public GitHub issue.**
2. Report the vulnerability privately via **[GitHub Private Vulnerability Reporting](https://github.com/domehahn/housekeeping/security/advisories/new)**.
3. If GitHub Advisories are not accessible, email details directly to the repository maintainer.

### When reporting, please include:
- A description of the vulnerability and potential impact.
- Steps to reproduce or a minimal proof of concept.
- The version(s) of `scm-cleaner` affected.
- Any suggested remediations or mitigations.

You will receive an acknowledgment within 48 hours, followed by updates on the fix and coordinated disclosure timeline.
