# Agent Governance Core — MVP Phase 1 Design Spec

**Date**: 2026-04-14
**Status**: Approved
**Scope**: Phase 1 MVP
**Consumer**: Sophia ecosystem (orchestrator TBD)
**Stack**: Go 1.26.2, PostgreSQL 16+, pgx v5, testify, testcontainers-go, oklog/ulid, slog

---

## 1. Repository Objective

`agent-governance-core` is the **control, decision, and execution supervision layer** of the Sophia ecosystem. Its responsibility is to guarantee that every agent action passes through an explicit, deterministic, and auditable governance pipeline before execution.

### Position in the Sophia Ecosystem

```
Consumers (CLI, API, orchestrators, agents)
           │ task intake
           ▼
┌────────────────────────────────┐
│    agent-governance-core       │
│                                │
│  routing → policy → approval   │
│  → workflow → audit            │
│                                │
│  Reads context ← memory-engine │
└────────────┬───────────────────┘
             │ execution decisions
             ▼
       runtime-adapters
  (git, shell, API calls, side effects)
```

### Governing Principle

> **Governance decides. Runtime executes. Memory informs.**

The core does not execute anything. It does not store knowledge. It does not reason about the user's problem. It guarantees that everything an agent wants to do passes through a pipeline where it is evaluated whether it can do it, how it must do it, within what limits, and that every decision is recorded.

### Governance Power

Governance controls HOW agents act, not WHAT they decide — but it CAN **block, condition, or escalate** actions that imply risk, side effects, or policy violations. Governance has veto power over dangerous actions.

### Design Scope

**Sophia-first, reuse-ready, not framework-first.**

- MVP optimized for the Sophia ecosystem as sole consumer in phase 1
- Clean ports and contracts that allow future reuse
- No generic framework abstractions
- Task/routing/workflow models designed for Sophia's real needs

### Responsibilities (Phase 1)

| Responsibility | Description |
|---|---|
| Task intake | Receive, validate, and classify tasks with risk and priority |
| Routing | Decide execution strategy and agent/mode assignment, explainably |
| Policy evaluation | Evaluate rules before allowing sensitive actions |
| Approval gates | Manage explicit HITL approval requests |
| Workflow orchestration | Advance tasks through a deterministic, auditable state machine |
| Execution budgets | Apply timeout and retry budgets with lease management |
| Kill switch | Terminate executions irrevocably |
| Escalation | Escalate to human or senior agent when conditions are met |
| Audit trail | Record every decision and transition append-only |
| Context consultation | Query memory-engine via port for enriched decisions |

### NOT Responsibilities

| Exclusion | Reason |
|---|---|
| Memory/knowledge persistence | That is memory-engine |
| Real side-effect execution | That is runtime-adapters |
| Retrieval, embeddings, indexing | That is memory-engine |
| Autonomous planning without controls | Contradicts governance purpose |
| Generic BPM engine | Over-engineering out of scope |
| Agent business decisions | Governance controls HOW, not WHAT |

---

## 2. Consumer Model

### Consumer: Future Sophia Orchestrator

The primary consumer is a Sophia orchestrator that **does not exist yet**. `agent-governance-core` is designed with clean contracts ready for the orchestrator to consume later, without absorbing orchestrator responsibilities.

### SDK-first

The primary contract is a **Go SDK** — a package exposing use cases as typed functions. HTTP is a secondary adapter for operability (dashboards, CLI, debugging).

| Factor | SDK | HTTP |
|---|---|---|
| Latency | In-process, zero overhead | Serialization + network |
| Type safety | Compile-time | Runtime |
| Phase 1 | Direct import | Requires separate service |
| Reuse | Can be wrapped in HTTP later | — |
| Operational complexity | Zero in phase 1 | Additional service |

### Consumer Contract

| Operation | Direction | Description |
|---|---|---|
| `SubmitTask` | Consumer → Core | Submit a task for governance |
| `ProcessTask` | Consumer → Core | Submit + full pipeline (convenience) |
| `GetWorkflowStatus` | Consumer → Core | Query workflow state |
| `ResolveApproval` | Consumer → Core | Approve or deny a request |
| `KillWorkflow` | Consumer → Core | Activate kill switch |
| `PauseWorkflow` | Consumer → Core | Pause execution |
| `ResumeWorkflow` | Consumer → Core | Resume paused execution |
| `QueryAudit` | Consumer → Core | Query audit trail |
| `OnExecutionReady` | Core → Consumer | Notify task ready for execution |
| `OnApprovalRequired` | Core → Consumer | Notify HITL approval needed |
| `OnWorkflowTerminated` | Core → Consumer | Notify workflow ended |

### Callbacks

The three `On*` events are **synchronous in-process callbacks** in phase 1. Not webhooks, not queues. If the consumer doesn't register callbacks, the core works fine — polling always available.

### Phase 1 Pragmatic Decisions

- SDK-first, HTTP secondary
- Synchronous callbacks (no event bus)
- Single consumer type (orchestrator)
- No multi-tenancy
- No auth between consumer and core (same trust boundary)

---

## 3. Domain Strategy

### Modular Monolith with Explicit Bounded Contexts

Not a flat monolith, not microservices. Each bounded context has its own subdirectory, entities, value objects, rules, and clear boundaries.

```
internal/domain/
  task/           — intake, classification, lifecycle
  routing/        — strategy decision and assignment
  policy/         — rule evaluation, outcomes
  approval/       — HITL approval gates
  workflow/       — execution state machine
  execution/      — lease management, budgets
  resilience/     — kill switch, circuit breaker (phase 2)
  escalation/     — escalation rules
  audit/          — append-only trail
  shared/         — transversal value objects and types
```

### Dependency Rules

| Context | Can depend on | CANNOT depend on |
|---|---|---|
| **Task** | `shared` | Nothing else — it is root |
| **Routing** | `task`, `shared` | policy, workflow, approval |
| **Policy** | `task`, `shared` | routing, workflow, approval |
| **Execution** | `task`, `shared` | routing, policy, workflow |
| **Workflow** | `task`, `routing` (decisions), `policy` (decisions), `execution` (leases), `shared` | approval, escalation directly |
| **Approval** | `task`, `shared` | workflow internals, routing, policy |
| **Escalation** | `task`, `shared` | workflow internals, approval internals |
| **Resilience** | `shared` | Everything — transversal, acts on IDs |
| **Audit** | `shared` | Everything — only receives, never queries |

### The Role of `shared`

Contains ONLY:

- Transversal value objects: `TaskID`, `WorkflowRunID`, `Timestamp`, `ActorID`, `RiskLevel`, `Priority`
- Shared domain types: status enums, outcome types
- Minimal interfaces for cross-context references

Does NOT contain business logic, entities, or use cases.

### Communication Between Contexts (Phase 1)

In-process and synchronous:

| Pattern | When used | Example |
|---|---|---|
| Direct type dependency | Context needs another's result | Workflow reads RoutingDecision |
| Port injection | Context queries another without coupling | Workflow receives PolicyChecker port |
| Audit recording | All contexts emit | Every use case receives AuditRecorder port |

No event bus, no CQRS, no sagas in phase 1.

### Workflow / Execution Separation

- **Workflow** = state, transitions, lifecycle invariants
- **Execution** = leases, timeout budgets, retry budgets, attempts
- Separated in domain, coordinated in application via `WorkflowRunService` in phase 1
- Workflow does NOT execute side effects
- Execution does NOT define business workflow states

---

## 4. Aggregates

### 4.1 Task (context: task)

| Field | Type | Description |
|---|---|---|
| `ID` | `TaskID` | Unique identifier (ULID) |
| `ParentTaskID` | `*TaskID` | Optional reference to parent (subtask relation) |
| `Type` | `TaskType` | Task type |
| `Title` | `string` | Short goal description |
| `Scope` | `TaskScope` | Scope (file, module, repo, system) |
| `Priority` | `Priority` | Assigned priority |
| `RiskLevel` | `RiskLevel` | Classified risk level |
| `Status` | `TaskStatus` | Lifecycle status |
| `Metadata` | `TaskMetadata` | Additional consumer context |
| `CreatedAt` | `Timestamp` | Creation |
| `UpdatedAt` | `Timestamp` | Last modification |

**Invariants**:
- Must have type, title, and scope to exist
- Status follows valid lifecycle
- RiskLevel and Priority assigned at intake

**Subtask model**: Reference-only (`ParentTaskID`). No inline children composition. Each subtask runs its own full governance pipeline independently. Parent/child coordination in application/read model if needed.

### 4.2 WorkflowRun (context: workflow)

| Field | Type | Description |
|---|---|---|
| `ID` | `WorkflowRunID` | Unique identifier (ULID) |
| `TaskID` | `TaskID` | Task reference |
| `Status` | `WorkflowStatus` | State machine status |
| `RoutingDecisionID` | `*RoutingDecisionID` | Routing decision reference |
| `PolicyDecisionID` | `*PolicyDecisionID` | Policy decision reference |
| `CurrentStepIndex` | `int` | Current step |
| `Transitions` | `[]WorkflowTransition` | Immutable transition history |
| `CreatedAt` | `Timestamp` | Creation |
| `UpdatedAt` | `Timestamp` | Last transition |

**Invariants**:
- Must reference an existing Task
- Cannot be running and awaiting_approval simultaneously
- Kill is terminal — no transitions out
- Completed, failed, killed are terminal
- Every transition recorded in history
- Only valid transitions per state machine table

### 4.3 ExecutionLease (context: execution)

| Field | Type | Description |
|---|---|---|
| `ID` | `ExecutionLeaseID` | Unique identifier (ULID) |
| `WorkflowRunID` | `WorkflowRunID` | Workflow reference |
| `TimeoutBudget` | `Duration` | Maximum total time allowed |
| `RetryBudget` | `RetryBudget` | Maximum retry attempts |
| `AttemptsUsed` | `int` | Consumed attempts |
| `TimeElapsed` | `Duration` | Consumed time |
| `Status` | `LeaseStatus` | active / exhausted / revoked |
| `CreatedAt` | `Timestamp` | Creation |

**Invariants**:
- AttemptsUsed cannot exceed RetryBudget
- Exhausted or revoked lease permits no further attempts
- Each attempt increments AttemptsUsed

**Phase 1 note**: Timeout budget keeps running during `paused` state. Phase 2 may add freeze/max_pause_duration.

### 4.4 RoutingDecision (context: routing)

| Field | Type | Description |
|---|---|---|
| `ID` | `RoutingDecisionID` | Unique identifier (ULID) |
| `TaskID` | `TaskID` | Task reference |
| `EvaluatedStrategies` | `[]StrategyEvaluation` | Strategies considered with scores |
| `SelectedStrategy` | `RoutingStrategy` | Chosen strategy |
| `SelectedAgentRole` | `AgentRole` | Assigned role/mode |
| `Reason` | `string` | Decision explanation |
| `Constraints` | `[]RoutingConstraint` | Optional constraints |
| `CreatedAt` | `Timestamp` | Creation |

**Invariants**:
- Must reference a Task
- Must have at least one evaluated strategy
- Must have selected strategy with reason
- **Immutable** post-creation (new decision = new record)

### 4.5 PolicyDecision (context: policy)

| Field | Type | Description |
|---|---|---|
| `ID` | `PolicyDecisionID` | Unique identifier (ULID) |
| `TaskID` | `TaskID` | Task reference |
| `EvaluatedAction` | `string` | Evaluated action |
| `Outcome` | `PolicyOutcome` | allow / allow_with_constraints / require_approval / deny |
| `Constraints` | `[]PolicyConstraint` | Constraints if applicable |
| `ApprovalRequirement` | `*ApprovalRequirement` | Requirement if applicable |
| `RulesEvaluated` | `[]RuleEvaluation` | Individual rule results |
| `Reason` | `string` | Explanation |
| `CreatedAt` | `Timestamp` | Creation |

**Invariants**:
- Must reference a Task
- Outcome must be explicit
- Deny is terminal for the evaluated action
- require_approval must include ApprovalRequirement
- allow_with_constraints must have at least one constraint
- **Immutable** post-creation

### 4.6 ApprovalRequest (context: approval)

| Field | Type | Description |
|---|---|---|
| `ID` | `ApprovalRequestID` | Unique identifier (ULID) |
| `TaskID` | `TaskID` | Task reference |
| `WorkflowRunID` | `WorkflowRunID` | Workflow reference |
| `Reason` | `string` | Why approval is required |
| `RequiredApprover` | `ApproverSpec` | Who can approve |
| `Status` | `ApprovalStatus` | pending / approved / denied / expired |
| `Resolution` | `*ApprovalResolution` | Resolution metadata |
| `ExpiresAt` | `*Timestamp` | Optional timeout |
| `CreatedAt` | `Timestamp` | Creation |

**Invariants**:
- Must reference Task and WorkflowRun
- Must have reason
- Can only be resolved once (pending → approved/denied/expired)
- Terminal states: approved, denied, expired
- Resolution must exist when status is not pending

### 4.7 EscalationTrigger (context: escalation)

| Field | Type | Description |
|---|---|---|
| `ID` | `EscalationTriggerID` | Unique identifier (ULID) |
| `TaskID` | `TaskID` | Task reference |
| `Condition` | `EscalationCondition` | Trigger condition |
| `Target` | `EscalationTarget` | Escalation target |
| `Status` | `EscalationStatus` | pending / triggered / resolved |
| `TriggeredAt` | `*Timestamp` | When triggered |
| `CreatedAt` | `Timestamp` | Creation |

**Invariants**:
- Must reference a Task
- Once triggered, must be resolved or recorded
- Condition must be evaluable

### 4.8 AuditEntry (context: audit)

| Field | Type | Description |
|---|---|---|
| `ID` | `AuditEntryID` | Unique identifier (ULID) |
| `TaskID` | `*TaskID` | Optional task reference |
| `WorkflowRunID` | `*WorkflowRunID` | Optional workflow reference |
| `Actor` | `ActorID` | Who performed the action |
| `Action` | `string` | What action |
| `Outcome` | `string` | Action result |
| `Context` | `AuditContext` | Structured metadata |
| `CreatedAt` | `Timestamp` | Immutable timestamp |

**Invariants**:
- Append-only — never edited, never deleted
- Must have actor, action, outcome, and timestamp

### Resilience Context

No aggregate in phase 1. Kill switch modeled as an operation on WorkflowRun (transition to killed). Circuit breaker is phase 2.

---

## 5. Value Objects

### ID Format: ULID

All IDs use ULID format (26 chars, Crockford base32) for consistency with memory-engine. Time-sortable, no PG extension required, generated in Go via `oklog/ulid/v2`.

### Enum Pattern: const string with validation

```go
type RiskLevel string

const (
    RiskLow      RiskLevel = "low"
    RiskMedium   RiskLevel = "medium"
    RiskHigh     RiskLevel = "high"
    RiskCritical RiskLevel = "critical"
)

func NewRiskLevel(s string) (RiskLevel, error) {
    switch RiskLevel(s) {
    case RiskLow, RiskMedium, RiskHigh, RiskCritical:
        return RiskLevel(s), nil
    default:
        return "", fmt.Errorf("invalid risk level: %s", s)
    }
}
```

Rules:
- Domain and application always use typed VOs
- Raw strings only enter/exit through adapters, DTOs, or persistence
- Every conversion from external string must pass through explicit validation

### Transversal VOs (domain/shared)

| VO | Go type | Validation | Used by |
|---|---|---|---|
| `TaskID` | `string` (ULID) | Non-empty, valid ULID | All |
| `WorkflowRunID` | `string` (ULID) | Non-empty, valid ULID | workflow, execution, approval, audit |
| `RoutingDecisionID` | `string` (ULID) | Non-empty, valid ULID | routing, workflow |
| `PolicyDecisionID` | `string` (ULID) | Non-empty, valid ULID | policy, workflow |
| `ApprovalRequestID` | `string` (ULID) | Non-empty, valid ULID | approval, workflow |
| `ExecutionLeaseID` | `string` (ULID) | Non-empty, valid ULID | execution, workflow |
| `EscalationTriggerID` | `string` (ULID) | Non-empty, valid ULID | escalation |
| `AuditEntryID` | `string` (ULID) | Non-empty, valid ULID | audit |
| `ActorID` | `string` | Non-empty | audit, approval |
| `Timestamp` | `time.Time` | Non-zero | All |
| `RiskLevel` | `string` (enum) | low, medium, high, critical | task, routing, policy |
| `Priority` | `string` (enum) | low, normal, high, urgent | task, routing |

### Local VOs by Context

#### task

| VO | Validation |
|---|---|
| `TaskType` | Enum: `development`, `review`, `research`, `refactor`, `bugfix`, `deployment` |
| `TaskScope` | Enum: `file`, `module`, `repo`, `system` |
| `TaskStatus` | Enum: `created`, `accepted`, `in_progress`, `completed`, `failed`, `cancelled` |
| `TaskMetadata` | map[string]any, immutable post-creation |

#### routing

| VO | Validation |
|---|---|
| `RoutingStrategy` | Enum: `direct`, `decompose`, `collaborate`, `escalate` |
| `AgentRole` | Enum: `implementer`, `reviewer`, `researcher`, `architect`, `human` |
| `StrategyEvaluation` | Strategy + Score (0.0–1.0) + FactorScores (map[string]float64) + Overridden (bool) + Reason |
| `RoutingConstraint` | Type + value |

#### policy

| VO | Validation |
|---|---|
| `PolicyOutcome` | Enum: `allow`, `allow_with_constraints`, `require_approval`, `deny` |
| `PolicyConstraint` | Type + value + reason |
| `ApprovalRequirement` | Approver spec + reason + optional timeout |
| `RuleEvaluation` | Rule ID + passed/failed + reason |

#### workflow

| VO | Validation |
|---|---|
| `WorkflowStatus` | Enum: `created`, `routed`, `policy_checked`, `running`, `awaiting_approval`, `approved`, `paused`, `completed`, `failed`, `killed` |
| `WorkflowTransition` | FromStatus + ToStatus + Reason + Timestamp + ActorID |

#### execution

| VO | Validation |
|---|---|
| `RetryBudget` | MaxAttempts (int > 0) |
| `LeaseStatus` | Enum: `active`, `exhausted`, `revoked` |
| `Duration` | time.Duration wrapper, > 0 |
| `AttemptStatus` | Enum: `success`, `failure`, `retry` |
| `AttemptResult` | Status + FailureStage* + FailureCode* + Retryable* + ToolName* + StrategyUsed* + AgentRole* + Detail* (see Failure Telemetry) |

#### approval

| VO | Validation |
|---|---|
| `ApprovalStatus` | Enum: `pending`, `approved`, `denied`, `expired` |
| `ApproverSpec` | Role or concrete ActorID |
| `ApprovalResolution` | ResolvedBy + ResolvedAt + Reason + Status |

#### escalation

| VO | Validation |
|---|---|
| `EscalationCondition` | Type + parameters |
| `EscalationTarget` | Enum: `human`, `senior_agent`, `admin` |
| `EscalationStatus` | Enum: `pending`, `triggered`, `resolved` |

#### audit

| VO | Validation |
|---|---|
| `AuditContext` | map[string]any, immutable |

#### shared (failure telemetry)

| VO | Validation |
|---|---|
| `FailureStage` | Enum: `routing`, `policy`, `approval`, `workflow`, `execution`, `runtime`, `memory_context`, `notification` |

### Failure Telemetry

`AttemptResult` is enriched to carry failure metadata for observability and future adaptive routing:

| Field | Type | Required | Description |
|---|---|---|---|
| `Status` | `AttemptStatus` | Yes | `success` / `failure` / `retry` |
| `FailureStage` | `*FailureStage` | Only on failure/retry | Where in the pipeline it failed |
| `FailureCode` | `*string` | Only on failure/retry | Convention: `category/detail` |
| `Retryable` | `*bool` | Only on failure/retry | Whether caller considers it retryable |
| `ToolName` | `*string` | Optional | Tool that failed |
| `StrategyUsed` | `*RoutingStrategy` | Optional | Strategy under which it executed |
| `AgentRole` | `*AgentRole` | Optional | Role of the executing agent |
| `Detail` | `*string` | Optional | Error message or detail |

**Invariants**:
- If Status is `success`, all failure fields must be nil
- If Status is `failure` or `retry`, FailureStage, FailureCode, and Retryable must be present

#### failure_code Taxonomy (documented convention)

Convention: `category/detail`

| Category | Examples | Description |
|---|---|---|
| `tool/*` | `tool/shell_timeout`, `tool/git_push_rejected`, `tool/file_permission_denied` | Specific tool failure |
| `runtime/*` | `runtime/connection_refused`, `runtime/process_killed`, `runtime/oom` | Infrastructure/runtime failure |
| `external_api/*` | `external_api/429`, `external_api/500`, `external_api/auth_expired` | External API failure |
| `memory/*` | `memory/context_unavailable`, `memory/timeout` | memory-engine consultation failure |
| `governance/*` | `governance/budget_exhausted`, `governance/lease_revoked` | Governance-level failure |
| `agent/*` | `agent/invalid_output`, `agent/hallucination_detected`, `agent/stuck` | Agent-level failure |

This taxonomy is extensible — new categories/details added without changing the FailureStage enum.

### AuditContext Reserved Keys

When use cases emit audit entries, the following keys are reserved in `AuditContext`:

| Key | Type | When present |
|---|---|---|
| `trace_id` | string | If OTel is configured (phase 2) |
| `span_id` | string | If OTel is configured (phase 2) |
| `failure_stage` | string | On failure/retry attempts |
| `failure_code` | string | On failure/retry attempts |
| `retryable` | bool | On failure/retry attempts |
| `tool_name` | string | If applicable |
| `strategy_used` | string | On RegisterAttempt (always) |
| `agent_role` | string | On RegisterAttempt (always) |
| `attempts_used` | int | On RegisterAttempt (always) |
| `retry_budget` | int | On RegisterAttempt (always) |
| `time_elapsed_ms` | int | On RegisterAttempt (always) |

### OTel Readiness (Phase 1 Prep)

Phase 1 does NOT depend on OpenTelemetry, but prepares for it:

1. **context.Context propagation**: Every use case, port, and adapter receives and propagates `ctx` without creating new contexts from scratch. Sub-contexts must derive via `context.WithTimeout(ctx, ...)` etc., never from `context.Background()`.
2. **trace_id / span_id extraction**: The `AuditRecorder` attempts to extract trace/span from `ctx`. If OTel is not configured (phase 1 default), the fields are absent. Zero hard dependency — conditional import of `go.opentelemetry.io/otel/trace`.
3. **No OTel port in phase 1**: Phase 2 will add `TracerProvider` port or use OTel global directly.

---

## 6. Use Cases

### Tier 1: Pipeline Core

#### UC-1: SubmitTask

**Package**: `application/intake`
**Input**: TaskType, Title, Scope, Metadata, suggested Priority
**Output**: Task with RiskLevel and Priority assigned

Flow:
1. Validate input
2. Classify risk (based on type + scope + metadata)
3. Assign priority (suggested or calculated)
4. Create Task in `created` status
5. Persist
6. Emit audit entry
7. Return Task

Memory-engine: Optional — may request context to enrich risk classification.

#### UC-2: RouteTask

**Package**: `application/routing`
**Input**: TaskID
**Output**: RoutingDecision

Flow:
1. Load Task
2. Query memory-engine for relevant context
3. Evaluate candidate strategies with scores
4. Select strategy + agent role
5. Create immutable RoutingDecision
6. Persist
7. Emit audit entry
8. If strategy is `decompose` → create subtasks (separate Tasks with ParentTaskID)
9. Return RoutingDecision

#### UC-3: EvaluatePolicy

**Package**: `application/policyeval`
**Input**: TaskID, EvaluatedAction
**Output**: PolicyDecision

Flow:
1. Load Task and RoutingDecision
2. Classify action sensitivity
3. Evaluate policy rules against the action
4. Produce outcome (most restrictive rule wins)
5. Create immutable PolicyDecision
6. Persist
7. Emit audit entry
8. Return PolicyDecision

#### UC-4: StartWorkflow

**Package**: `application/workflowrun`
**Input**: TaskID
**Output**: WorkflowRun + ExecutionLease

Flow:
1. Verify Task exists
2. Verify RoutingDecision exists
3. Verify PolicyDecision exists
4. If deny → transition to `failed`
5. If require_approval → transition to `awaiting_approval`, create ApprovalRequest
6. If allow/allow_with_constraints → transition to `running`
7. Create ExecutionLease with budgets
8. Persist
9. Emit audit entries per transition
10. Notify consumer via callback
11. Return WorkflowRun

**Note**: This UC coordinates Workflow and Execution in application layer.

#### UC-5: RecordAuditEntry

**Package**: `application/audit`
**Input**: Actor, Action, Outcome, TaskID (optional), WorkflowRunID (optional), Context
**Output**: Persisted AuditEntry

Internal service consumed by other use cases via port.

### UC-C: ProcessTask (Composite)

**Package**: `application/intake`
**Input**: Same as SubmitTask
**Output**: ProcessTaskResult (Task + RoutingDecision + PolicyDecision + WorkflowRun)

Executes: SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow in sequence.

**Rules**:
- Does NOT bypass any governance step
- Does NOT collapse aggregates
- Does NOT hide intermediate results
- Produces audit trail at each stage

### Tier 2: Operational Control

#### UC-6: ResolveApproval

**Package**: `application/approvals`
**Input**: ApprovalRequestID, Resolution, ResolvedBy, Reason
**Output**: Resolved ApprovalRequest + transitioned WorkflowRun

#### UC-7: KillWorkflow

**Package**: `application/workflowrun`
**Input**: WorkflowRunID, Reason, Actor
**Output**: WorkflowRun in `killed` state

Kill is terminal. Revokes ExecutionLease.

#### UC-8: PauseWorkflow / ResumeWorkflow

**Package**: `application/workflowrun`

Pause: running → paused. Resume: paused → running (if lease not exhausted).

#### UC-9: RegisterAttempt

**Package**: `application/workflowrun`
**Input**: WorkflowRunID, AttemptResult (enriched with failure telemetry — see section 5)
**Output**: Updated ExecutionLease + WorkflowRun transition if applicable

Flow:
1. Load WorkflowRun + ExecutionLease
2. Validate AttemptResult (failure fields present if status != success)
3. Increment AttemptsUsed on lease
4. If success → transition workflow to `completed`
5. If failure and retry budget available → maintain `running`
6. If failure and budget exhausted → transition to `failed`
7. Emit audit entry with enriched AuditContext (strategy_used, agent_role, failure telemetry, lease state)
8. Return updated WorkflowRun

### Tier 3: Observability

#### UC-10: GetWorkflowStatus

**Package**: `application/workflowrun`

#### UC-11: QueryAuditTrail

**Package**: `application/audit`
**Input**: Filters (TaskID, WorkflowRunID, Actor, DateRange, Action)
**Output**: Paginated []AuditEntry

#### UC-12: TriggerEscalation

**Package**: `application/escalation`

Simple in phase 1: timeout_exceeded, retries_exhausted, risk_critical.

### Pipeline End-to-End

```
SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow
                                              │
                              ┌────────────────┼───────────────┐
                              ▼                ▼               ▼
                         allow →        require_approval →   deny →
                         running        awaiting_approval    failed
                              │                │
                              │          ResolveApproval
                              ▼                ▼
                    RegisterAttempt ←── running (approved)
                         │
                  ┌──────┼──────┐
                  ▼      ▼      ▼
             completed  retry  failed

  [Anytime: KillWorkflow, PauseWorkflow, TriggerEscalation]
  [Always: RecordAuditEntry at every step]
```

---

## 7. Ports

### Inbound Ports

Four separated ports + one facade for consumer ergonomics.

#### GovernanceService

```
SubmitTask(ctx, input)              → (Task, error)
ProcessTask(ctx, input)             → (ProcessTaskResult, error)
RouteTask(ctx, TaskID)              → (RoutingDecision, error)
EvaluatePolicy(ctx, TaskID, action) → (PolicyDecision, error)
StartWorkflow(ctx, TaskID)          → (WorkflowRun, error)
```

#### WorkflowControl

```
KillWorkflow(ctx, WorkflowRunID, reason, actor)   → error
PauseWorkflow(ctx, WorkflowRunID, reason, actor)   → error
ResumeWorkflow(ctx, WorkflowRunID, reason, actor)  → error
RegisterAttempt(ctx, WorkflowRunID, result)         → (WorkflowRun, error)
```

#### ApprovalService

```
ResolveApproval(ctx, ApprovalRequestID, resolution) → (ApprovalRequest, error)
GetPendingApprovals(ctx, filters)                    → ([]ApprovalRequest, error)
```

#### QueryService

```
GetTask(ctx, TaskID)                        → (Task, error)
GetWorkflowStatus(ctx, WorkflowRunID)       → (WorkflowRun, error)
GetWorkflowByTask(ctx, TaskID)              → (WorkflowRun, error)
QueryAuditTrail(ctx, filters)               → ([]AuditEntry, Pagination, error)
```

#### GovernanceFacade

Composes all four inbound ports into a single entry point for consumer convenience. Lives as a composition struct, not as a replacement for the granular ports.

### Outbound Ports

#### Repositories (one per aggregate)

| Port | Key operations |
|---|---|
| `TaskRepository` | Save, FindByID, FindByParentID, UpdateStatus |
| `WorkflowRunRepository` | Save, FindByID, FindByTaskID, Update |
| `ExecutionLeaseRepository` | Save, FindByWorkflowRunID, Update |
| `RoutingDecisionRepository` | Save, FindByTaskID |
| `PolicyDecisionRepository` | Save, FindByTaskID |
| `ApprovalRequestRepository` | Save, FindByID, FindByTaskID, FindPending, Update |
| `EscalationTriggerRepository` | Save, FindByTaskID, Update |
| `AuditEntryRepository` | Append, Query(filters, pagination) |

#### MemoryContextProvider

```
GetRelevantContext(ctx, TaskID, query)    → (MemoryContext, error)
GetTaskHistory(ctx, scope, filters)       → ([]MemoryEntry, error)
GetHeuristics(ctx, domain, action)        → ([]Heuristic, error)
```

**Degradable**: If memory-engine is unavailable, adapter returns empty context without error. Governance works without memory, just with less information.

#### GovernanceNotifier

```
OnExecutionReady(ctx, WorkflowRun, ExecutionLease)   → error
OnApprovalRequired(ctx, WorkflowRun, ApprovalRequest) → error
OnWorkflowTerminated(ctx, WorkflowRun, reason)        → error
```

Synchronous in-process callbacks in phase 1. Optional — polling always works.

#### Clock

```
Now() → Timestamp
```

Port for testability — control time in tests without sleep.

#### IDGenerator

```
NewTaskID() → TaskID
NewWorkflowRunID() → WorkflowRunID
// ... etc for each ID type
```

Port so ULID generation lives in adapter, not domain.

### Port → Adapter Mapping (Phase 1)

| Outbound port | Adapter |
|---|---|
| `*Repository` | PostgreSQL (pgx v5) |
| `MemoryContextProvider` | memory-engine SDK |
| `GovernanceNotifier` | In-process callbacks |
| `Clock` | `time.Now()` wrapper |
| `IDGenerator` | ULID via `oklog/ulid/v2` |

| Inbound port | Adapter |
|---|---|
| All inbound (primary) | Go SDK (direct import) |
| All inbound (secondary) | HTTP REST |

---

## 8. Persistence — PostgreSQL

### Strategy

One table per aggregate root. Value objects as columns or JSONB for variable collections. No ORM — `pgx` v5 direct.

### Schema

#### tasks

```sql
CREATE TABLE tasks (
    id              VARCHAR(26) PRIMARY KEY,
    parent_task_id  VARCHAR(26) REFERENCES tasks(id),
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    scope           TEXT NOT NULL,
    priority        TEXT NOT NULL,
    risk_level      TEXT NOT NULL,
    status          TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_parent ON tasks(parent_task_id) WHERE parent_task_id IS NOT NULL;
CREATE INDEX idx_tasks_type ON tasks(type);
```

#### workflow_runs

```sql
CREATE TABLE workflow_runs (
    id                   VARCHAR(26) PRIMARY KEY,
    task_id              VARCHAR(26) NOT NULL REFERENCES tasks(id),
    status               TEXT NOT NULL,
    routing_decision_id  VARCHAR(26) REFERENCES routing_decisions(id),
    policy_decision_id   VARCHAR(26) REFERENCES policy_decisions(id),
    current_step_index   INTEGER NOT NULL DEFAULT 0,
    transitions          JSONB NOT NULL DEFAULT '[]',
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_workflow_runs_task ON workflow_runs(task_id);
CREATE INDEX idx_workflow_runs_status ON workflow_runs(status);
```

#### execution_leases

```sql
CREATE TABLE execution_leases (
    id                VARCHAR(26) PRIMARY KEY,
    workflow_run_id   VARCHAR(26) NOT NULL REFERENCES workflow_runs(id),
    timeout_budget_ms BIGINT NOT NULL,
    retry_budget      INTEGER NOT NULL,
    attempts_used     INTEGER NOT NULL DEFAULT 0,
    time_elapsed_ms   BIGINT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX idx_execution_leases_workflow ON execution_leases(workflow_run_id);
```

#### routing_decisions

```sql
CREATE TABLE routing_decisions (
    id                   VARCHAR(26) PRIMARY KEY,
    task_id              VARCHAR(26) NOT NULL REFERENCES tasks(id),
    evaluated_strategies JSONB NOT NULL,
    selected_strategy    TEXT NOT NULL,
    selected_agent_role  TEXT NOT NULL,
    reason               TEXT NOT NULL,
    constraints          JSONB NOT NULL DEFAULT '[]',
    created_at           TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_routing_decisions_task ON routing_decisions(task_id);
```

**Immutable**: No UPDATE on this table.

#### policy_decisions

```sql
CREATE TABLE policy_decisions (
    id                    VARCHAR(26) PRIMARY KEY,
    task_id               VARCHAR(26) NOT NULL REFERENCES tasks(id),
    evaluated_action      TEXT NOT NULL,
    outcome               TEXT NOT NULL,
    constraints           JSONB NOT NULL DEFAULT '[]',
    approval_requirement  JSONB,
    rules_evaluated       JSONB NOT NULL DEFAULT '[]',
    reason                TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_policy_decisions_task ON policy_decisions(task_id);
CREATE INDEX idx_policy_decisions_outcome ON policy_decisions(outcome);
```

**Immutable**: No UPDATE on this table.

#### approval_requests

```sql
CREATE TABLE approval_requests (
    id                VARCHAR(26) PRIMARY KEY,
    task_id           VARCHAR(26) NOT NULL REFERENCES tasks(id),
    workflow_run_id   VARCHAR(26) NOT NULL REFERENCES workflow_runs(id),
    reason            TEXT NOT NULL,
    required_approver JSONB NOT NULL,
    status            TEXT NOT NULL,
    resolution        JSONB,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_approval_requests_task ON approval_requests(task_id);
CREATE INDEX idx_approval_requests_status ON approval_requests(status);
CREATE INDEX idx_approval_requests_pending ON approval_requests(status) WHERE status = 'pending';
```

#### escalation_triggers

```sql
CREATE TABLE escalation_triggers (
    id            VARCHAR(26) PRIMARY KEY,
    task_id       VARCHAR(26) NOT NULL REFERENCES tasks(id),
    condition     JSONB NOT NULL,
    target        TEXT NOT NULL,
    status        TEXT NOT NULL,
    triggered_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_escalation_triggers_task ON escalation_triggers(task_id);
CREATE INDEX idx_escalation_triggers_status ON escalation_triggers(status);
```

#### audit_entries

```sql
CREATE TABLE audit_entries (
    id              VARCHAR(26) PRIMARY KEY,
    task_id         VARCHAR(26),
    workflow_run_id VARCHAR(26),
    actor           TEXT NOT NULL,
    action          TEXT NOT NULL,
    outcome         TEXT NOT NULL,
    context         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_audit_entries_task ON audit_entries(task_id) WHERE task_id IS NOT NULL;
CREATE INDEX idx_audit_entries_workflow ON audit_entries(workflow_run_id) WHERE workflow_run_id IS NOT NULL;
CREATE INDEX idx_audit_entries_actor ON audit_entries(actor);
CREATE INDEX idx_audit_entries_created ON audit_entries(created_at);
```

**Append-only**: No UPDATE, no DELETE. Ever.

### JSONB Strategy

| Use | Example | Reason |
|---|---|---|
| Variable VO collections | transitions, strategies, rules, constraints | Avoids child tables for data always read with parent |
| Nested objects | resolution, approval_requirement, condition | Known structure, no independent queries needed |
| Flexible metadata | metadata, context | Free schema in phase 1 |

Rule: If phase 2 requires filtering/JOIN on JSONB data, extract to table.

### Migrations

- SQL files in `migrations/postgres/`
- Numbered: `001_create_tasks.sql`, `002_create_workflow_runs.sql`, etc.
- Each has UP and DOWN
- Applied via golang-migrate or similar
- No automatic migrations in application code

### Driver

`pgx` v5 direct. No ORM. sqlc evaluable for phase 2.

---

## 9. Routing / Policy / Workflow Strategy

### 9.1 Routing

**Mechanism**: Score-based, deterministic, Go code rules.

Strategies phase 1: `direct`, `decompose`, `escalate`. (`collaborate` documented, not implemented.)

#### Hard Overrides (evaluated first)

Before scoring, deterministic overrides short-circuit the process:

| Condition | Override | Reason |
|---|---|---|
| `risk_level == critical` | → `escalate` | Critical risk always escalates |
| `scope == system && type == deployment` | → `escalate` | System-wide deploy always escalates |
| Consumer sends `force_strategy` in metadata | → specified strategy | Explicit consumer override (auditable) |

Overrides are evaluated in order. If any matches, scoring is skipped. The override is recorded in `RoutingDecision.Reason` with `[override]` tag.

**Invariant**: The tiebreaker (below) can NEVER contradict a hard override, nor weaken a security decision made later in the pipeline (policy/approval).

#### Score Formula

**Weighted sum** — each factor contributes a weighted score per strategy:

```
score(strategy) = Σ (factor_weight × factor_score(strategy))
```

Where:
- `factor_score` is a value between 0.0 and 1.0 (0 = does not apply, 1 = perfect match)
- `factor_weight` is a configurable weight per factor

Default weights (phase 1, constants in Go code):

| Factor | Weight | Justification |
|---|---|---|
| risk_level | 0.30 | Risk is the most determining factor |
| task_scope | 0.25 | Scope determines complexity |
| task_type | 0.20 | Type informs strategy |
| task_priority | 0.10 | Priority influences but does not determine |
| memory_similar_tasks | 0.10 | History enriches but does not dominate |
| memory_heuristics | 0.05 | Weak signal, complementary |

**Total**: 1.00. Weights are constants in Go code in phase 1. Configurable in phase 2.

#### Tiebreaker

If two strategies have the same final score:

1. **Prefer the less risky strategy**: `direct` > `decompose` > `escalate`
2. If still tied: **prefer the simplest** (direct)

Rationale: In ambiguity, the system favors controlled direct execution. If there is real reason to escalate or decompose, scores will reflect it.

**Invariant**: The tiebreaker NEVER contradicts a hard override, NOR weakens a security decision downstream in the pipeline. Tiebreaker only resolves score-based ambiguity between strategies.

#### Scoring Rules

Example factor scoring (Go functions):

```
risk_level == critical             → escalate factor_score = 1.0
scope == system && type == deployment → escalate factor_score = 0.9
scope == repo && type == refactor    → decompose factor_score = 0.8
scope == file && type == bugfix      → direct factor_score = 0.9
similar tasks failed historically    → escalate factor_score += 0.2
default                              → direct factor_score = 0.5
```

Rules are Go functions, not DSL/YAML. Pure functions: Task + MemoryContext → []StrategyEvaluation.

### 9.2 Policy

**Mechanism**: Sequential rule evaluation. Most restrictive wins.

Restrictiveness hierarchy:
```
deny > require_approval > allow_with_constraints > allow
```

Policy rules phase 1:

| Rule | Condition | Outcome |
|---|---|---|
| `risk_critical_requires_approval` | risk == critical | require_approval |
| `deployment_requires_approval` | type == deployment | require_approval |
| `system_scope_requires_approval` | scope == system | require_approval |
| `destructive_action_deny` | destructive action without explicit flag | deny |
| `file_scope_low_risk_allow` | scope == file && risk == low | allow |
| `default_allow_with_constraints` | No other rule matched | allow_with_constraints |

Rules implemented as `PolicyRule` interface in Go:

```go
type PolicyRule interface {
    ID() string
    Evaluate(task Task, action string, context PolicyContext) RuleEvaluation
}
```

Sensitive action classification: static map in phase 1.

| Category | Examples | Sensitive |
|---|---|---|
| File read | Read file, search code | No |
| File write | Create/edit file | Depends on scope |
| Shell execution | Run command | Yes |
| Git push | Push to remote | Yes |
| External API | Call external service | Yes |
| Deployment | Deploy to any env | Always |
| Data deletion | Delete data, drop tables | Always |

### 9.3 Workflow

**Mechanism**: Explicit transition table, not if/else.

```
Created → Routed → PolicyChecked → Running → Completed
                                  ↘ AwaitingApproval → Approved → Running
                                  ↘ Paused → Running
                                  ↘ Failed
                                  ↘ Killed (terminal)
```

Transition table:

| From | To | Condition |
|---|---|---|
| Created | Routed | Routing decision exists |
| Routed | PolicyChecked | Policy decision exists |
| PolicyChecked | Running | Policy allows |
| PolicyChecked | AwaitingApproval | Policy requires approval |
| PolicyChecked | Failed | Policy denies |
| AwaitingApproval | Approved | Approval granted |
| AwaitingApproval | Failed | Approval denied/timeout |
| Approved | Running | Always |
| Running | Completed | Execution succeeds |
| Running | Failed | Budget exhausted |
| Running | Paused | Explicit pause |
| Running | Killed | Kill switch |
| Paused | Running | Resume |
| Paused | Killed | Kill switch |
| *any non-terminal* | Killed | Kill switch |

Kill validated separately via `IsTerminal()` check.

Each transition creates an immutable `WorkflowTransition` VO added to the history.

### Workflow + ExecutionLease Coordination (in application)

| Event | Workflow | ExecutionLease |
|---|---|---|
| Start (allow) | → running | Create with budgets |
| Attempt (success) | → completed | Reference |
| Attempt (fail, budget) | Stays running | Increment attempts |
| Attempt (fail, exhausted) | → failed | → exhausted |
| Kill | → killed | → revoked |
| Pause | → paused | No change (clock keeps running) |
| Resume | → running | Verify not exhausted |

---

## 10. Testing Strategy

### Levels

| Level | What | Where | Tools | DB |
|---|---|---|---|---|
| Unit | Aggregates, VOs, domain rules, transitions, policy/routing rules | `internal/domain/*_test.go` | testify | No |
| Application | Use cases with mocked ports | `internal/application/*_test.go` | testify + interface mocks | No |
| Integration | PG repos, queries, migrations | `test/integration/` | testify + testcontainers-go | Real PG |
| Use-case integration | Full pipeline with real PG | `test/integration/usecases/` | testify + testcontainers-go | Real PG |
| E2E | HTTP adapter + full pipeline | `test/e2e/` | testify + testcontainers-go + HTTP | Real PG |

### Unit Tests (Densest Level)

Domain aggregates and transitions:
- Every valid transition. Every invalid transition rejected.
- Kill from every non-terminal state. Transition from terminal fails.
- Lease budget exhaustion. Attempt on exhausted/revoked lease fails.
- Task creation with valid/invalid fields. Status lifecycle.
- Approval resolution. Double-resolution fails.
- VO validation (only valid enum values, ULID format).

**Mandatory no-bypass tests**:
- No StartWorkflow without RoutingDecision
- No StartWorkflow without PolicyDecision
- No ResolveApproval outside pending
- No RegisterAttempt on terminal workflow
- No continuation after kill

Policy rules:
- Each rule individually. PolicyEvaluator with multiple rules (most restrictive wins).
- Action classification correctness.

Routing rules:
- Each scoring rule. Full evaluator. With and without memory context.

### Application Tests

Use cases with interface-based mocks (not implementation mocks):
- SubmitTask: Task created, audit emitted, memory consulted.
- RouteTask: Decision created, workflow transitioned, decompose creates subtasks.
- EvaluatePolicy: Decision created, correct branching by outcome.
- StartWorkflow: Correct branching (allow/require_approval/deny).
- ProcessTask: Full pipeline, all steps audited.
- ResolveApproval, KillWorkflow, RegisterAttempt: All branches.

### Integration Tests

testcontainers-go with real PostgreSQL:

```
test/
  integration/
    testhelpers/
      pg.go                    — testcontainers PG setup + migration runner
    persistence/
      task_repo_test.go
      workflow_run_repo_test.go
      ...
    usecases/
      process_task_test.go     — end-to-end with real PG
```

Core scenarios:
- Happy path: allow → completed
- Approval flow: require_approval → approved → completed
- Deny flow: deny → failed
- Kill mid-execution
- Budget exhausted
- Decompose creates subtasks

### Test Fixtures

Functional options pattern in `test/fixtures/`:

```go
func NewTask(opts ...TaskOption) Task { ... }
func WithRiskLevel(r RiskLevel) TaskOption { ... }
func WithScope(s TaskScope) TaskOption { ... }
```

### Failure Telemetry Tests

| What | Test |
|---|---|
| AttemptResult validation | success → failure fields nil. failure → FailureStage/Code/Retryable required. |
| FailureStage enum | Only valid stages accepted. Invalid string rejected. |
| RegisterAttempt audit context | Enriched AuditContext includes strategy_used, agent_role, failure data, lease state. |
| AuditContext trace extraction | With mock OTel span → trace_id/span_id present. Without → absent. |
| Routing hard overrides | Critical risk → escalate regardless of scores. force_strategy honored. |
| Routing tiebreaker | Equal scores → direct wins. Override always beats tiebreaker. |
| Routing score formula | Weighted sum produces expected scores. Weights sum to 1.0. |

### Coverage Targets

| Level | Target |
|---|---|
| Domain | >90% |
| Application | >85% |
| Integration | >80% |
| Use-case integration | All core scenarios |

### Tools

| Tool | Use |
|---|---|
| `testing` stdlib | Base |
| `testify/assert` + `require` | Assertions |
| `testcontainers-go` + postgres module | Real PG |
| `golangci-lint` | Linting |
| Interface-based mocks (manual or `moq`) | Port mocking |

---

## 11. Phase 1 Boundary

### Excluded from Phase 1

**Routing**: `collaborate` strategy, adaptive routing, DSL/YAML rules, capacity constraints.

**Policy**: DSL/YAML rules, versioning, inheritance, dynamic loading, ABAC/RBAC.

**Workflow**: Multi-step with branching, compensation/rollback, templates, timeout freeze during pause, sagas.

**Execution**: Circuit breaker, rate limiting, preemption, cost budgets.

**Escalation**: Multi-level chains, pattern-based auto-escalation, SLA tracking.

**Approval**: Multi-approver (N of M), delegation, templates.

**Audit**: Retention policies, bulk export, alerting, compliance reports.

**Infrastructure**: Event bus, CQRS, multi-tenancy, auth, observability (beyond slog), gRPC, WebSocket/SSE, horizontal scaling.

**Memory-engine integration**: Feedback loop (sending execution results back), heuristic creation, bi-directional sync.

### Implementation Rule

If a subagent encounters a case that "naturally" needs a phase 2 feature during implementation:

1. Do NOT implement it
2. Leave a `// Phase 2:` comment in code
3. Document the case in the PR
4. Continue with phase 1 scope

Scope is not negotiated during implementation.

### Note for Phase 2 Evolution

The system must leave sufficient audit/telemetry data to enable adaptive routing evolution in phase 2.

### Phase 2 Backlog

| Item | Description | Depends on |
|---|---|---|
| DLQ / failed task quarantine | Tasks that fail repeatedly go to quarantine instead of retrying indefinitely | Failure telemetry |
| Policy decision cache | Cache PolicyDecisions for identical actions within a time window | Policy evaluator |
| OpenTelemetry complete | Instrument spans in each UC, export to collector | OTel readiness prep (phase 1) |
| Routing adaptativo | Adjust scores based on failure history per strategy/agent/tool | Failure telemetry + memory-engine feedback loop |

---

## 12. Repository Configuration

### CLAUDE.md Updates

The existing CLAUDE.md is valid and aligned with this spec. Updates needed:

- Add ULID as explicit ID standard
- Add pgx v5 as driver
- Reference this spec document
- Add `ProcessTask` composite UC to the "what this repository does" list

### AGENTS.md (Skills Index)

The existing 13 skills remain valid:

| Skill | Applies to |
|---|---|
| `architecture-guardrails` | Boundary changes, new contexts |
| `task-modeling` | Task aggregate, intake UC |
| `routing-strategy` | Routing rules, evaluator, strategies |
| `workflow-state-machine` | Workflow transitions, state management |
| `policy-evaluation` | Policy rules, evaluator, outcomes |
| `approval-gates` | Approval aggregate, HITL flows |
| `resilience-controls` | Execution leases, kill switch, budgets |
| `escalation-modeling` | Escalation triggers, conditions |
| `audit-trail` | AuditEntry, append-only recording |
| `memory-integration` | MemoryContextProvider port/adapter |
| `persistence-postgres` | All repositories, migrations, pgx |
| `api-contracts` | Inbound ports, HTTP adapter, SDK |
| `testing-quality` | All test levels, fixtures, coverage |

### rules.md Updates

Existing rules are valid. Add:

- ULID as ID format standard
- `// Phase 2:` comment convention for out-of-scope features
- ProcessTask composite must not bypass individual pipeline steps

### Skill Content Updates

Each skill SKILL.md should be updated to reference:

- The specific aggregates/VOs it governs
- The invariants it must protect
- The test patterns it must follow
- The phase 1 boundary for its domain

These updates happen mechanically during implementation — each subagent implementing a domain block also updates its skill.

---

## 13. SDD + Subagent Driven Development Preparation

### Implementation Block Decomposition

The implementation is divided into independent blocks that can be executed by specialized subagents:

| Block | Depends on | Skill(s) | Subagent scope |
|---|---|---|---|
| **B1: shared VOs** | Nothing | architecture-guardrails | All shared VOs, ID types, enums, validation |
| **B2: task domain** | B1 | task-modeling | Task aggregate, TaskMetadata, lifecycle |
| **B3: routing domain** | B1 | routing-strategy | RoutingDecision, StrategyEvaluation, routing rules |
| **B4: policy domain** | B1 | policy-evaluation | PolicyDecision, PolicyRule interface, rules, evaluator |
| **B5: workflow domain** | B1 | workflow-state-machine | WorkflowRun, transition table, transitions |
| **B6: execution domain** | B1 | resilience-controls | ExecutionLease, budgets, lease status |
| **B7: approval domain** | B1 | approval-gates | ApprovalRequest, resolution, status |
| **B8: escalation domain** | B1 | escalation-modeling | EscalationTrigger, conditions |
| **B9: audit domain** | B1 | audit-trail | AuditEntry, append-only |
| **B10: outbound ports** | B1-B9 | architecture-guardrails | All port interfaces |
| **B11: persistence** | B10 | persistence-postgres | All PG repos, migrations |
| **B12: memory adapter** | B10 | memory-integration | MemoryContextProvider adapter |
| **B13: inbound ports** | B1-B9 | api-contracts | Inbound port interfaces |
| **B14: application — intake** | B2, B10 | task-modeling | SubmitTask UC |
| **B15: application — routing** | B3, B10, B12 | routing-strategy | RouteTask UC |
| **B16: application — policy** | B4, B10 | policy-evaluation | EvaluatePolicy UC |
| **B17: application — workflow** | B5, B6, B10 | workflow-state-machine, resilience-controls | StartWorkflow, Kill, Pause, Resume, RegisterAttempt UCs |
| **B18: application — approvals** | B7, B10 | approval-gates | ResolveApproval UC |
| **B19: application — escalation** | B8, B10 | escalation-modeling | TriggerEscalation UC |
| **B20: application — audit** | B9, B10 | audit-trail | RecordAuditEntry, QueryAuditTrail UCs |
| **B21: application — composite** | B14-B20 | architecture-guardrails | ProcessTask UC |
| **B22: facade** | B13, B14-B21 | api-contracts | GovernanceFacade |
| **B23: HTTP adapter** | B13 | api-contracts | REST handlers |
| **B24: test fixtures** | B1-B9 | testing-quality | Factories for all aggregates |
| **B25: domain unit tests** | B2-B9, B24 | testing-quality | All domain tests |
| **B26: application tests** | B14-B21, B24 | testing-quality | All UC tests |
| **B27: integration tests** | B11, B24 | testing-quality, persistence-postgres | Repo tests with real PG |
| **B28: use-case integration** | B14-B21, B11, B24 | testing-quality | Pipeline e2e with real PG |
| **B29: wiring** | All | architecture-guardrails | cmd/main.go, DI, config |

### Dependency Graph (Parallelization)

```
B1 (shared VOs)
 ├── B2 (task) ──────────────────┐
 ├── B3 (routing) ───────────────┤
 ├── B4 (policy) ────────────────┤
 ├── B5 (workflow) ──────────────┤
 ├── B6 (execution) ─────────────┤
 ├── B7 (approval) ──────────────┤
 ├── B8 (escalation) ────────────┤
 └── B9 (audit) ─────────────────┤
                                  ▼
                        B10 (outbound ports)
                        B13 (inbound ports)
                        B24 (test fixtures)
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
         B11 (persistence) B12 (memory)  B14-B20 (app UCs)
              │                               │
              ▼                               ▼
         B27 (integ tests)              B21 (composite)
                                        B22 (facade)
                                        B23 (HTTP)
                                              │
                                              ▼
                                   B25 (domain tests)
                                   B26 (app tests)
                                   B28 (UC integ tests)
                                              │
                                              ▼
                                        B29 (wiring)
```

### Parallel Execution Groups

| Group | Blocks | Can run in parallel |
|---|---|---|
| **G1** | B1 | Foundation — must be first |
| **G2** | B2, B3, B4, B5, B6, B7, B8, B9 | All domain aggregates in parallel |
| **G3** | B10, B13, B24 | Ports + fixtures in parallel |
| **G4** | B11, B12, B14, B15, B16, B17, B18, B19, B20 | Persistence + app UCs in parallel |
| **G5** | B21, B22, B23, B25, B26 | Composite + facade + HTTP + domain/app tests |
| **G6** | B27, B28 | Integration tests |
| **G7** | B29 | Wiring — last |

### Checkpoints

After each group completes:

1. **G1 complete**: VOs compile, enum validation works
2. **G2 complete**: All aggregates compile, domain invariants enforced
3. **G3 complete**: Port interfaces defined, fixtures usable
4. **G4 complete**: UCs execute, repos persist, memory adapter works
5. **G5 complete**: Full pipeline works via SDK and HTTP, all unit/app tests pass
6. **G6 complete**: Integration tests pass with real PG
7. **G7 complete**: Application boots, full end-to-end operational

### Subagent Rules

1. Each subagent receives: block scope, relevant skill(s), this spec as reference
2. Subagents do NOT implement phase 2 features — `// Phase 2:` comment only
3. Subagents do NOT modify code outside their block scope
4. Each subagent must leave its block with passing tests (where applicable)
5. Each subagent updates the skill SKILL.md if its implementation reveals patterns not yet documented

### Brainstorming Preparation

The brainstorming phase for this project is now **complete**. All 13 sections have been discussed, debated, and closed. The decisions captured in this spec document represent the converged design.

If during implementation a subagent encounters a genuine architectural contradiction (not a phase 2 feature request), it should:

1. Stop implementation of the affected block
2. Document the contradiction
3. Escalate to the orchestrator session for resolution

Brainstorming is NOT reopened for:
- Preference changes
- "Nicer" alternatives discovered during implementation
- Phase 2 scope that seems easy to add

Brainstorming IS reopened for:
- Two invariants that contradict each other
- A technical impossibility discovered during implementation
- A dependency cycle that the design did not anticipate

---

## Appendix A: Key Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Sophia-first, reuse-ready, not framework-first | MVP optimized for Sophia, clean ports for future reuse |
| D2 | SDK-first, HTTP secondary | In-process in phase 1, zero overhead |
| D3 | Orchestrator comes later | Core designed with contracts, not absorbing orchestrator role |
| D4 | Synchronous callbacks, no event bus | Sufficient for in-process phase 1 |
| D5 | Modular monolith with bounded contexts | Clean separation without microservice overhead |
| D6 | Workflow/Execution separated in domain, coordinated in application | Clean domain, pragmatic application |
| D7 | Subtasks by reference (ParentTaskID) | Independent pipelines, no composition in aggregate |
| D8 | ULID for all IDs | Consistent with memory-engine, time-sortable |
| D9 | Enums as const string with validation | Legible in logs/JSON, type-safe in Go |
| D10 | ProcessTask composite UC | Convenience without bypassing governance steps |
| D11 | 4 inbound ports + facade | Granular internally, ergonomic for consumers |
| D12 | MemoryContextProvider is degradable | Core works without memory-engine |
| D13 | pgx v5 direct, no ORM | Control, explicit queries, no magic |
| D14 | JSONB for variable collections | Pragmatic phase 1, extractable to tables |
| D15 | Routing/policy rules as Go code, not DSL | Testable, type-safe, sufficient for MVP |
| D16 | Score-based deterministic routing | Explainable, auditable, reproducible |
| D17 | Policy: most restrictive rule wins | Simple, predictable, unambiguous |
| D18 | State machine as explicit transition table | No hidden states, validatable |
| D19 | Timeout keeps running during pause | Simple, prevents zombie workflows |
| D20 | Immutable RoutingDecision and PolicyDecision | Auditability — new decision = new record |
| D21 | Append-only audit, never update/delete | Non-negotiable governance trail |
| D22 | testcontainers with real PG, not SQLite | PG-specific features (JSONB, partial indexes) |
| D23 | Coverage: domain >90%, app >85%, integration >80% | Domain invariants are the highest priority |
| D24 | Phase 2 features get `// Phase 2:` comment only | Scope not negotiated during implementation |
| D25 | OTel readiness: context propagation + trace_id/span_id in AuditContext | Zero dependency in phase 1, ready for phase 2 instrumentation |
| D26 | Failure telemetry: FailureStage enum + failure_code convention | Hybrid model — stage is finite/typed, code is flexible with category/detail convention |
| D27 | Routing score formula: weighted sum, tiebreaker favors simplest, hard overrides first | Explainable, deterministic, tiebreaker never contradicts overrides or security decisions |
| D28 | Phase 2 backlog: DLQ, policy cache, OTel complete, routing adaptativo | Documented, not implemented — depends on phase 1 telemetry foundation |
