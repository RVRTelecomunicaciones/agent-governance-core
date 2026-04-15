# Project Rules

## 1. Repository purpose

This repository implements a reusable governance core for AI agent execution systems.

It is responsible for:

- task intake
- routing decisions
- workflow orchestration
- policy decisions
- approval requests
- timeout/retry controls
- kill switches
- escalation
- audit

It is NOT responsible for:

- long-term memory persistence
- retrieval/indexing
- embeddings
- runtime adapter implementation details
- git/PR execution details
- document storage

## 2. Core invariants

1. A task must exist before a workflow run exists.
2. A routing decision must exist before execution starts.
3. A policy decision must exist before sensitive execution starts.
4. A workflow cannot be both running and awaiting approval.
5. A killed workflow is terminal.
6. Approval-pending workflows cannot execute side effects.
7. Every governance-relevant action must produce audit data.
8. Retry and timeout budgets must be explicit.
9. Memory-engine is consumed through ports only.
10. Runtime execution is downstream of governance, not embedded inside it.

## 3. Architecture rules

Use clean / hexagonal architecture.

Required layers:

- domain
- application
- ports
- adapters
- infrastructure

Rules:

- domain must not depend on infrastructure
- policy logic must not live in HTTP handlers
- workflow transitions must not be ad hoc
- adapters must be thin
- repositories must be ports
- execution details must not leak into domain entities

## 4. Workflow rules

- workflow transitions must be explicit
- invalid transitions must fail
- pause/approval/kill must be modeled directly
- retries must consume retry budget
- timeouts must be enforceable

## 5. Policy rules

- policy outcomes must be explicit:
  - allow
  - allow_with_constraints
  - require_approval
  - deny
- policy must be auditable
- sensitive actions must be identifiable
- a denied action must not proceed

## 6. Approval rules

- approvals must be explicit records
- approvals must have reason and status
- approval resolution must be auditable
- approval timeout/escalation must be possible

## 7. Audit rules

- audit is append-only
- audit entries must include actor, action, outcome and timestamp
- governance decisions must be traceable

## 8. Resilience rules

- every execution path must have timeout budget
- every retryable path must have retry budget
- kill switch must be terminal
- circuit breakers and escalation may be phase 2, but phase 1 must be ready for them

## 9. Simplicity rules

Do not:

- overengineer phase 1
- introduce distributed orchestration too early
- build a generic BPM engine
- mix free-form agent reasoning into governance decisions without controls
- embed policy logic into runtime adapters
