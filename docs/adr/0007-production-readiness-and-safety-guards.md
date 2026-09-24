# ADR 0007: Production Readiness and Hardened Safety Guards

* **Status:** Accepted
* **Date:** 2026-09-08
* **Authors:** Antigravity / Principal DevSecOps & SRE Reviewer

## Context

`scm-cleaner` operates on live production infrastructure, deleting/archiving projects, blocking users, and mutating pipeline or runner tags. To achieve enterprise production readiness, the tool must enforce strict fail-closed safety models, prevent out-of-scope bypasses, guard against stale or tampered plan execution, ensure crash-resilient audit logging, and require explicit multi-factor confirmations for high-blast-radius operations.

## Architecture & Safety Decisions

### 1. Universal Out-of-Scope Runner Mutation Impact Evaluation
* **Decision:** Every runner action (`ActionAddRunnerTag`, `ActionReplaceRunnerTag`) dynamically reports out-of-scope impact.
* **Mechanism:** `PlannedAction.OutOfScopeImpact()` evaluates out-of-scope assigned project paths. Execution is blocked unless `--confirm-out-of-scope-impact=N` matches the total out-of-scope impact across all runner actions in both interactive and non-interactive runs.

### 2. Mandatory Live Rediscovery and Fail-Closed Percentage Guards
* **Decision:** Percentage safety guards (`max_percentage`) are evaluated against both the static plan discovery snapshot AND a fresh live population rediscovery at execute time (`CheckGuards`).
* **Mechanism:** If live scope population rediscovery fails or if the denominator is missing, percentage guard checks fail closed (`ExitSafetyGuardTriggered`), preventing execution under network degradation or partial API responses.

### 3. Plan Freshness, Stale Plan Guard, and Config Digest Integrity
* **Decision:** Plans have a configurable maximum lifetime (`max_plan_age`, default 24h) and embed a canonical SHA-256 `ConfigDigest` over safety-relevant configuration fields.
* **Mechanism:** Execution rejects stale plans (`ExitStalePlan`, code 8) unless overridden via `--allow-stale-plan=<hash>`. Configuration changes between planning and execution trigger an immediate abort (`ExitConfigDrift`, code 9).

### 4. Incremental, Crash-Resilient Audit Logging
* **Decision:** Audit records are written incrementally per action before (`phase: intent`) and after (`phase: result`) invocation, accompanied by an explicit `fsync()` to disk.
* **Mechanism:** If `audit.required_for_apply` is set or `--apply` is passed without an audit path configured, execution aborts with `ExitInvalidConfiguration`. Audit logs capture plan metadata, tool version, commit, authenticated actor, and execution outcome.

### 5. Explicit Confirmation for Instance-Wide User Blocks
* **Decision:** Blocking users carries instance-wide impact beyond group boundaries.
* **Mechanism:** In non-interactive mode (`--non-interactive`), plans containing `ActionBlockUser` require `--confirm-instance-wide-user-block`. In interactive mode, operators must type `block N users instance-wide`.

### 6. Atomic Plan Persistence and Ed25519 Plan Signing
* **Decision:** Plan files are saved atomically (`os.CreateTemp`, `f.Sync()`, `os.Chmod(0600)`, `os.Rename`) to eliminate partial write risks.
* **Mechanism:** Plans support optional Ed25519 signature verification (`SignPlan` and `VerifyPlanSignature`) to protect supply chains against tampered plan files.

### 7. Self-Check Readiness (`scm-cleaner doctor`)
* **Decision:** `scm-cleaner doctor` performs comprehensive pre-flight verification covering TLS security mode, configuration validation, config digest calculation, safety limits, audit sink availability, API connectivity, and permissions.
* **Mechanism:** Doctor returns exit code 0 when all checks pass and non-zero when misconfigurations or permission issues exist.

## Consequences

* Operators gain guaranteed auditability, tamper detection, and fail-closed safety guards.
* CI/CD pipelines executing `scm-cleaner` are protected against stale plans, configuration drift, and unconfirmed out-of-scope mutations.
* System reliability and compliance requirements for enterprise environments are fully satisfied.

