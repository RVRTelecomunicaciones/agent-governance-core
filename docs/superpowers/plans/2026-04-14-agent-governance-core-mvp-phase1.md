# Agent Governance Core MVP Phase 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the governance layer for the Sophia ecosystem — task intake, score-based routing, policy evaluation, approval gates, deterministic workflow state machine, execution budgets, kill switch, escalation, and append-only audit trail.

**Architecture:** Clean/hexagonal architecture with modular monolith bounded contexts. SDK-first (Go package), HTTP secondary. Sophia-first design with reuse-ready ports. Workflow and Execution separated in domain, coordinated in application via WorkflowRunService.

**Tech Stack:** Go 1.26.2, PostgreSQL 16+, pgx v5, chi router, testify, testcontainers-go, oklog/ulid v2, slog, golangci-lint

**Spec:** `docs/superpowers/specs/2026-04-14-agent-governance-core-mvp-phase1-design.md`

**Implementation matices (carry throughout):**
1. `WorkflowStatus` is source of truth for execution state — `TaskStatus` must NOT duplicate workflow state
2. `RecordAuditEntry` is a transversal app service, not an independent business feature
3. `MemoryContextProvider` degradable with clear traceability when unavailable
4. `AuditContext` needs builder helpers to prevent chaotic `map[string]any` drift
5. `ProcessTask` stays convenience, not pseudo-orchestrator

---

## Block Execution Map

| Task | Block(s) | Group | Parallel with | Skill(s) | Done criteria | Checkpoint |
|---|---|---|---|---|---|---|
| **T1: Repo alignment + Shared VOs** | B1 | G1 | None (foundation) | architecture-guardrails | `go test ./internal/domain/shared/...` passes, `go build ./...` succeeds, go.mod at 1.26.2 | G1: VOs compile, enums validate |
| **T2: Task domain** | B2 | G2 | T3, T4, T5, T6 | task-modeling | `go test ./internal/domain/task/...` passes | — |
| **T3: Workflow + Execution** | B5+B6 | G2 | T2, T4, T5, T6 | workflow-state-machine, resilience-controls | `go test ./internal/domain/{workflow,execution}/...` passes | — |
| **T4: Routing domain** | B3 | G2 | T2, T3, T5, T6 | routing-strategy | `go test ./internal/domain/routing/...` passes, overrides + tiebreaker + scoring verified | — |
| **T5: Policy domain** | B4 | G2 | T2, T3, T4, T6 | policy-evaluation | `go test ./internal/domain/policy/...` passes, most-restrictive-wins verified | — |
| **T6: Approval + Escalation + Audit** | B7+B8+B9 | G2 | T2, T3, T4, T5 | approval-gates, escalation-modeling, audit-trail | `go test ./internal/domain/{approval,escalation,audit}/...` passes | G2: All aggregates compile, invariants enforced |
| **T7: Ports** | B10+B13 | G3 | T8, T9 | architecture-guardrails, api-contracts | `go build ./...` succeeds with all port interfaces | — |
| **T8: Clock + IDGenerator** | infra | G3 | T7, T9 | architecture-guardrails | `go build ./...` succeeds | — |
| **T9: Test fixtures** | B24 | G3 | T7, T8 | testing-quality | Factories compile and produce valid aggregates | G3: Ports defined, infra ready, fixtures usable |
| **T10: Migrations** | B11 partial | G4 | T11, T12, T13 | persistence-postgres | All .sql files present with UP/DOWN | — |
| **T11: PG Repositories** | B11 | G4 | T12, T13 | persistence-postgres | `go build ./...` succeeds with all repo implementations | — |
| **T12: Memory adapter** | B12 | G4 | T11, T13 | memory-integration | `go build ./...` succeeds, degradable stub logs when unavailable | — |
| **T13: Callback notifier** | events | G4 | T11, T12 | architecture-guardrails | `go build ./...` succeeds | — |
| **T14: App — Audit recorder** | B20 | G4 | — | audit-trail | `go test ./internal/application/audit/...` passes | — |
| **T15: App — Intake** | B14 | G4 | T16 | task-modeling | `go test ./internal/application/intake/...` passes | — |
| **T16: App — Routing + Policy** | B15+B16 | G4 | T15 | routing-strategy, policy-evaluation | `go test ./internal/application/{routing,policyeval}/...` passes | — |
| **T17: App — Workflow + Approvals + Escalation** | B17+B18+B19 | G4 | — | workflow-state-machine, approval-gates, escalation-modeling | `go test ./internal/application/{workflowrun,approvals,escalation}/...` passes, no-bypass tests pass | G4: Full pipeline works via SDK |
| **T18: Facade + HTTP** | B22+B23 | G5 | — | api-contracts | `go build ./...` succeeds, HTTP router wired | G5: SDK facade + HTTP operational |
| **T19: Integration test infra** | B27 | G6 | — | testing-quality, persistence-postgres | testcontainers PG boots, migrations apply, repo roundtrips pass | — |
| **T20: Use-case integration** | B28 | G6 | — | testing-quality | All 6 core scenarios pass with real PG | G6: Full integration suite green |
| **T21: Wiring** | B29 | G7 | — | architecture-guardrails | `make build && make run` boots, `make test` green | G7: Application operational end-to-end |

### Dependency Graph

```
T1 (shared VOs + repo alignment)
 ├── T2 (task) ─────────────┐
 ├── T3 (workflow+execution) ┤
 ├── T4 (routing) ───────────┤ ← All G2 tasks run in parallel
 ├── T5 (policy) ────────────┤
 └── T6 (approval+esc+audit) ┘
         │
    T7 (ports) ─── T8 (clock+idgen) ─── T9 (fixtures)  ← G3 in parallel
         │
    T10 (migrations)
    T11 (PG repos) ─── T12 (memory) ─── T13 (notifier) ← G4 partial parallel
         │
    T14 (audit svc) → T15 (intake) ─── T16 (routing+policy) → T17 (workflow+approvals)
         │
    T18 (facade + HTTP)   ← G5
         │
    T19 (integ infra) → T20 (UC integ tests)   ← G6
         │
    T21 (wiring)   ← G7
```

---

## File Structure

```
go.mod                                           — update to Go 1.26.2, add dependencies
internal/
  domain/
    shared/
      ids.go                                     — all typed ID wrappers (TaskID, WorkflowRunID, etc.)
      ids_test.go                                — ULID validation tests
      enums.go                                   — RiskLevel, Priority, FailureStage
      enums_test.go                              — enum validation tests
      timestamp.go                               — Timestamp wrapper
      errors.go                                  — shared domain errors
    task/
      task.go                                    — Task aggregate root
      task_test.go                               — Task invariant tests
    workflow/
      workflow_run.go                            — WorkflowRun aggregate, transition table
      workflow_run_test.go                       — transition tests (densest test file)
      status.go                                  — WorkflowStatus enum, IsTerminal()
      transition.go                              — WorkflowTransition VO
    execution/
      execution_lease.go                         — ExecutionLease aggregate
      execution_lease_test.go                    — budget/lease tests
      attempt.go                                 — AttemptResult, AttemptStatus, failure telemetry VOs
      attempt_test.go                            — attempt result validation tests
    routing/
      routing_decision.go                        — RoutingDecision aggregate
      strategy.go                                — RoutingStrategy, AgentRole, StrategyEvaluation VOs
      rules.go                                   — scoring rules (Go functions)
      evaluator.go                               — RoutingEvaluator (overrides → scoring → tiebreaker)
      evaluator_test.go                          — scoring, override, tiebreaker tests
    policy/
      policy_decision.go                         — PolicyDecision aggregate
      outcome.go                                 — PolicyOutcome, PolicyConstraint, RuleEvaluation VOs
      rule.go                                    — PolicyRule interface
      rules.go                                   — concrete phase 1 rules
      evaluator.go                               — PolicyEvaluator (sequential, most restrictive wins)
      sensitivity.go                             — action sensitivity classification map
      evaluator_test.go                          — policy evaluation tests
    approval/
      approval_request.go                        — ApprovalRequest aggregate
      approval_request_test.go                   — resolution, double-resolution tests
    escalation/
      escalation_trigger.go                      — EscalationTrigger aggregate
      escalation_trigger_test.go                 — condition, status tests
    audit/
      audit_entry.go                             — AuditEntry aggregate (append-only)
      audit_context.go                           — AuditContext builder helpers
      audit_context_test.go                      — builder tests
  ports/
    inbound/
      governance_service.go                      — GovernanceService interface
      workflow_control.go                        — WorkflowControl interface
      approval_service.go                        — ApprovalService interface
      query_service.go                           — QueryService interface
    outbound/
      repositories.go                            — all 8 repository interfaces
      memory_context_provider.go                 — MemoryContextProvider interface
      governance_notifier.go                     — GovernanceNotifier callback interface
      clock.go                                   — Clock interface
      id_generator.go                            — IDGenerator interface
      audit_recorder.go                          — AuditRecorder interface (transversal)
  application/
    intake/
      submit_task.go                             — SubmitTask use case
      process_task.go                            — ProcessTask composite use case
    routing/
      route_task.go                              — RouteTask use case
    policyeval/
      evaluate_policy.go                         — EvaluatePolicy use case
    workflowrun/
      service.go                                 — WorkflowRunService (coordinates workflow + execution)
      start_workflow.go                          — StartWorkflow use case
      kill_workflow.go                           — KillWorkflow use case
      pause_resume.go                            — PauseWorkflow, ResumeWorkflow
      register_attempt.go                        — RegisterAttempt use case
      get_status.go                              — GetWorkflowStatus use case
    approvals/
      resolve_approval.go                        — ResolveApproval use case
    escalation/
      trigger_escalation.go                      — TriggerEscalation use case
    audit/
      record_audit.go                            — RecordAuditEntry (transversal service)
      query_audit.go                             — QueryAuditTrail use case
  adapters/
    outbound/
      persistence/
        pg_task_repo.go                          — TaskRepository PostgreSQL implementation
        pg_workflow_run_repo.go                  — WorkflowRunRepository
        pg_execution_lease_repo.go               — ExecutionLeaseRepository
        pg_routing_decision_repo.go              — RoutingDecisionRepository
        pg_policy_decision_repo.go               — PolicyDecisionRepository
        pg_approval_request_repo.go              — ApprovalRequestRepository
        pg_escalation_trigger_repo.go            — EscalationTriggerRepository
        pg_audit_entry_repo.go                   — AuditEntryRepository
      memory/
        memory_context_adapter.go                — MemoryContextProvider stub adapter
      events/
        callback_notifier.go                     — GovernanceNotifier in-process callbacks
    inbound/
      http/
        router.go                                — chi router setup
        task_handler.go                           — task HTTP handlers
        workflow_handler.go                       — workflow HTTP handlers
        approval_handler.go                       — approval HTTP handlers
        audit_handler.go                          — audit HTTP handlers
      sdk/
        facade.go                                — GovernanceFacade composing all inbound ports
  infrastructure/
    config/
      config.go                                  — application configuration
    database/
      postgres.go                                — pgx pool setup
    clock/
      real_clock.go                              — Clock implementation
    idgen/
      ulid_generator.go                          — IDGenerator ULID implementation
migrations/
  postgres/
    001_create_tasks.sql
    002_create_routing_decisions.sql
    003_create_policy_decisions.sql
    004_create_workflow_runs.sql
    005_create_execution_leases.sql
    006_create_approval_requests.sql
    007_create_escalation_triggers.sql
    008_create_audit_entries.sql
test/
  fixtures/
    task_factory.go                              — Task test factory
    workflow_factory.go                          — WorkflowRun test factory
    routing_factory.go                           — RoutingDecision test factory
    policy_factory.go                            — PolicyDecision test factory
    approval_factory.go                          — ApprovalRequest test factory
    execution_factory.go                         — ExecutionLease test factory
    escalation_factory.go                        — EscalationTrigger test factory
    audit_factory.go                             — AuditEntry test factory
  integration/
    testhelpers/
      pg.go                                      — testcontainers PG setup + migration runner
    persistence/
      task_repo_test.go
      workflow_run_repo_test.go
      execution_lease_repo_test.go
      routing_decision_repo_test.go
      policy_decision_repo_test.go
      approval_request_repo_test.go
      audit_entry_repo_test.go
    usecases/
      process_task_test.go                       — full pipeline e2e with real PG
cmd/
  agent-governance-core/
    main.go                                      — wiring, DI, server startup
```

---

## Task 1: Repo Alignment + Shared Value Objects (G1: B1)

**Skill:** architecture-guardrails

**Files:**
- Modify: `go.mod` — update to Go 1.26.2, add all dependencies
- Modify: `Makefile` — add `test-integration` target, verify existing targets
- Create: `internal/domain/shared/ids.go`
- Create: `internal/domain/shared/ids_test.go`
- Create: `internal/domain/shared/enums.go`
- Create: `internal/domain/shared/enums_test.go`
- Create: `internal/domain/shared/timestamp.go`
- Create: `internal/domain/shared/errors.go`

**Done criteria:** `go test ./internal/domain/shared/...` passes. `go build ./...` succeeds. go.mod shows Go 1.26.2. Makefile has `test-integration` target.

This task builds the foundation that ALL other tasks depend on. Starts with repo alignment (go.mod, Makefile), then shared VOs.

- [ ] **Step 0: Align repo — update go.mod, verify Makefile, clean scaffold**

Update go.mod to Go 1.26.2. Add `test-integration` target to Makefile. Remove any doc.go files that will be replaced by real code in subsequent tasks. Verify the directory structure matches the file structure section above.

```bash
cd /Users/russell/Documents/2026/agent-governance-core
sed -i '' 's/go 1.23/go 1.26.2/' go.mod
```

Add to Makefile:
```makefile
test-integration:
	go test ./test/integration/... -v -race -count=1 -tags=integration
```

- [ ] **Step 1: Install dependencies**

```bash
cd /Users/russell/Documents/2026/agent-governance-core
sed -i '' 's/go 1.23/go 1.26.2/' go.mod
go get github.com/oklog/ulid/v2@latest
go get github.com/jackc/pgx/v5@latest
go get github.com/go-chi/chi/v5@latest
go get github.com/stretchr/testify@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
go get go.opentelemetry.io/otel/trace@latest
go mod tidy
```

- [ ] **Step 2: Write failing tests for ID types**

```go
// internal/domain/shared/ids_test.go
package shared_test

import (
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskID_Valid(t *testing.T) {
	id, err := shared.NewTaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP")
	require.NoError(t, err)
	assert.Equal(t, shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"), id)
	assert.Equal(t, "01HZXK5V6R3QW0F8YJ9N2TMGCP", id.String())
}

func TestTaskID_Empty(t *testing.T) {
	_, err := shared.NewTaskID("")
	assert.ErrorIs(t, err, shared.ErrEmptyID)
}

func TestTaskID_InvalidFormat(t *testing.T) {
	_, err := shared.NewTaskID("not-a-ulid")
	assert.ErrorIs(t, err, shared.ErrInvalidULID)
}

func TestWorkflowRunID_Valid(t *testing.T) {
	id, err := shared.NewWorkflowRunID("01HZXK5V6R3QW0F8YJ9N2TMGCP")
	require.NoError(t, err)
	assert.Equal(t, "01HZXK5V6R3QW0F8YJ9N2TMGCP", id.String())
}

func TestDifferentIDTypes_NotInterchangeable(t *testing.T) {
	// This test exists to document that TaskID and WorkflowRunID
	// are distinct types at compile time. The compiler enforces this.
	var taskID shared.TaskID = "01HZXK5V6R3QW0F8YJ9N2TMGCP"
	_ = taskID
	// var wfID shared.WorkflowRunID = taskID // ← this would not compile
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/domain/shared/... -v -run TestTaskID`
Expected: FAIL — types not defined

- [ ] **Step 4: Implement ID types**

```go
// internal/domain/shared/ids.go
package shared

import (
	"github.com/oklog/ulid/v2"
)

// TaskID uniquely identifies a governance task.
type TaskID string

func NewTaskID(s string) (TaskID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return TaskID(s), nil
}

func (id TaskID) String() string { return string(id) }

// WorkflowRunID uniquely identifies a workflow run.
type WorkflowRunID string

func NewWorkflowRunID(s string) (WorkflowRunID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return WorkflowRunID(s), nil
}

func (id WorkflowRunID) String() string { return string(id) }

// RoutingDecisionID uniquely identifies a routing decision.
type RoutingDecisionID string

func NewRoutingDecisionID(s string) (RoutingDecisionID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return RoutingDecisionID(s), nil
}

func (id RoutingDecisionID) String() string { return string(id) }

// PolicyDecisionID uniquely identifies a policy decision.
type PolicyDecisionID string

func NewPolicyDecisionID(s string) (PolicyDecisionID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return PolicyDecisionID(s), nil
}

func (id PolicyDecisionID) String() string { return string(id) }

// ApprovalRequestID uniquely identifies an approval request.
type ApprovalRequestID string

func NewApprovalRequestID(s string) (ApprovalRequestID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return ApprovalRequestID(s), nil
}

func (id ApprovalRequestID) String() string { return string(id) }

// ExecutionLeaseID uniquely identifies an execution lease.
type ExecutionLeaseID string

func NewExecutionLeaseID(s string) (ExecutionLeaseID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return ExecutionLeaseID(s), nil
}

func (id ExecutionLeaseID) String() string { return string(id) }

// EscalationTriggerID uniquely identifies an escalation trigger.
type EscalationTriggerID string

func NewEscalationTriggerID(s string) (EscalationTriggerID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return EscalationTriggerID(s), nil
}

func (id EscalationTriggerID) String() string { return string(id) }

// AuditEntryID uniquely identifies an audit entry.
type AuditEntryID string

func NewAuditEntryID(s string) (AuditEntryID, error) {
	if err := validateULID(s); err != nil {
		return "", err
	}
	return AuditEntryID(s), nil
}

func (id AuditEntryID) String() string { return string(id) }

// ActorID identifies who performed an action.
type ActorID string

func NewActorID(s string) (ActorID, error) {
	if s == "" {
		return "", ErrEmptyID
	}
	return ActorID(s), nil
}

func (id ActorID) String() string { return string(id) }

func validateULID(s string) error {
	if s == "" {
		return ErrEmptyID
	}
	if _, err := ulid.ParseStrict(s); err != nil {
		return ErrInvalidULID
	}
	return nil
}
```

- [ ] **Step 5: Run ID tests to verify they pass**

Run: `go test ./internal/domain/shared/... -v -run TestTaskID`
Expected: PASS

- [ ] **Step 6: Write failing tests for enums**

```go
// internal/domain/shared/enums_test.go
package shared_test

import (
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskLevel_Valid(t *testing.T) {
	tests := []string{"low", "medium", "high", "critical"}
	for _, v := range tests {
		rl, err := shared.NewRiskLevel(v)
		require.NoError(t, err)
		assert.Equal(t, shared.RiskLevel(v), rl)
	}
}

func TestRiskLevel_Invalid(t *testing.T) {
	_, err := shared.NewRiskLevel("extreme")
	assert.Error(t, err)
}

func TestPriority_Valid(t *testing.T) {
	tests := []string{"low", "normal", "high", "urgent"}
	for _, v := range tests {
		p, err := shared.NewPriority(v)
		require.NoError(t, err)
		assert.Equal(t, shared.Priority(v), p)
	}
}

func TestPriority_Invalid(t *testing.T) {
	_, err := shared.NewPriority("mega")
	assert.Error(t, err)
}

func TestFailureStage_Valid(t *testing.T) {
	stages := []string{"routing", "policy", "approval", "workflow", "execution", "runtime", "memory_context", "notification"}
	for _, v := range stages {
		fs, err := shared.NewFailureStage(v)
		require.NoError(t, err)
		assert.Equal(t, shared.FailureStage(v), fs)
	}
}

func TestFailureStage_Invalid(t *testing.T) {
	_, err := shared.NewFailureStage("unknown")
	assert.Error(t, err)
}
```

- [ ] **Step 7: Implement enums**

```go
// internal/domain/shared/enums.go
package shared

import "fmt"

// RiskLevel classifies the risk of a task.
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
		return "", fmt.Errorf("invalid risk level: %q", s)
	}
}

func (r RiskLevel) String() string { return string(r) }

// Priority indicates task priority.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func NewPriority(s string) (Priority, error) {
	switch Priority(s) {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return Priority(s), nil
	default:
		return "", fmt.Errorf("invalid priority: %q", s)
	}
}

func (p Priority) String() string { return string(p) }

// FailureStage indicates where in the pipeline a failure occurred.
type FailureStage string

const (
	StageRouting       FailureStage = "routing"
	StagePolicy        FailureStage = "policy"
	StageApproval      FailureStage = "approval"
	StageWorkflow      FailureStage = "workflow"
	StageExecution     FailureStage = "execution"
	StageRuntime       FailureStage = "runtime"
	StageMemoryContext FailureStage = "memory_context"
	StageNotification  FailureStage = "notification"
)

func NewFailureStage(s string) (FailureStage, error) {
	switch FailureStage(s) {
	case StageRouting, StagePolicy, StageApproval, StageWorkflow,
		StageExecution, StageRuntime, StageMemoryContext, StageNotification:
		return FailureStage(s), nil
	default:
		return "", fmt.Errorf("invalid failure stage: %q", s)
	}
}

func (f FailureStage) String() string { return string(f) }
```

- [ ] **Step 8: Implement Timestamp and errors**

```go
// internal/domain/shared/timestamp.go
package shared

import "time"

// Timestamp wraps time.Time for domain use.
type Timestamp struct {
	time.Time
}

func NewTimestamp(t time.Time) (Timestamp, error) {
	if t.IsZero() {
		return Timestamp{}, ErrZeroTimestamp
	}
	return Timestamp{Time: t}, nil
}

func MustTimestamp(t time.Time) Timestamp {
	ts, err := NewTimestamp(t)
	if err != nil {
		panic(err)
	}
	return ts
}
```

```go
// internal/domain/shared/errors.go
package shared

import "errors"

var (
	ErrEmptyID       = errors.New("id must not be empty")
	ErrInvalidULID   = errors.New("id must be a valid ULID")
	ErrZeroTimestamp  = errors.New("timestamp must not be zero")
)
```

- [ ] **Step 9: Run all shared tests**

Run: `go test ./internal/domain/shared/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 10: Commit**

```bash
git add internal/domain/shared/ go.mod go.sum
git commit -m "feat: add shared value objects — typed IDs, enums, timestamp"
```

**Checkpoint G1**: shared VOs compile, all enum validation works, ULID validation works.

---

## Task 2: Task Domain Aggregate (G2: B2)

**Files:**
- Create: `internal/domain/task/task.go`
- Create: `internal/domain/task/task_test.go`

- [ ] **Step 1: Write failing tests for Task creation and lifecycle**

```go
// internal/domain/task/task_test.go
package task_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTask_Valid(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	tk, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		task.TypeDevelopment,
		"Implement auth module",
		task.ScopeModule,
		shared.PriorityNormal,
		shared.RiskMedium,
		nil, // no parent
		task.TaskMetadata{"repo": "agent-governance-core"},
		now,
	)
	require.NoError(t, err)
	assert.Equal(t, task.StatusCreated, tk.Status())
	assert.Equal(t, "Implement auth module", tk.Title())
	assert.Nil(t, tk.ParentTaskID())
}

func TestNewTask_MissingTitle(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	_, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		task.TypeDevelopment,
		"", // empty title
		task.ScopeModule,
		shared.PriorityNormal,
		shared.RiskMedium,
		nil,
		nil,
		now,
	)
	assert.Error(t, err)
}

func TestNewTask_WithParent(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	parentID := shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP")
	tk, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		task.TypeDevelopment,
		"Subtask: implement handler",
		task.ScopeFile,
		shared.PriorityNormal,
		shared.RiskLow,
		&parentID,
		nil,
		now,
	)
	require.NoError(t, err)
	require.NotNil(t, tk.ParentTaskID())
	assert.Equal(t, parentID, *tk.ParentTaskID())
}

func TestTask_AcceptTransition(t *testing.T) {
	tk := validTask(t)
	err := tk.Accept(shared.MustTimestamp(time.Now()))
	require.NoError(t, err)
	assert.Equal(t, task.StatusAccepted, tk.Status())
}

func TestTask_InvalidTransition(t *testing.T) {
	tk := validTask(t)
	// Cannot go from created directly to completed
	err := tk.Complete(shared.MustTimestamp(time.Now()))
	assert.Error(t, err)
}

func validTask(t *testing.T) *task.Task {
	t.Helper()
	now := shared.MustTimestamp(time.Now())
	tk, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		task.TypeDevelopment,
		"Test task",
		task.ScopeFile,
		shared.PriorityNormal,
		shared.RiskLow,
		nil,
		nil,
		now,
	)
	require.NoError(t, err)
	return tk
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/task/... -v`
Expected: FAIL — types not defined

- [ ] **Step 3: Implement Task aggregate**

```go
// internal/domain/task/task.go
package task

import (
	"errors"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// TaskType classifies the kind of work.
type TaskType string

const (
	TypeDevelopment TaskType = "development"
	TypeReview      TaskType = "review"
	TypeResearch    TaskType = "research"
	TypeRefactor    TaskType = "refactor"
	TypeBugfix      TaskType = "bugfix"
	TypeDeployment  TaskType = "deployment"
)

func NewTaskType(s string) (TaskType, error) {
	switch TaskType(s) {
	case TypeDevelopment, TypeReview, TypeResearch, TypeRefactor, TypeBugfix, TypeDeployment:
		return TaskType(s), nil
	default:
		return "", fmt.Errorf("invalid task type: %q", s)
	}
}

func (t TaskType) String() string { return string(t) }

// TaskScope defines the breadth of impact.
type TaskScope string

const (
	ScopeFile   TaskScope = "file"
	ScopeModule TaskScope = "module"
	ScopeRepo   TaskScope = "repo"
	ScopeSystem TaskScope = "system"
)

func NewTaskScope(s string) (TaskScope, error) {
	switch TaskScope(s) {
	case ScopeFile, ScopeModule, ScopeRepo, ScopeSystem:
		return TaskScope(s), nil
	default:
		return "", fmt.Errorf("invalid task scope: %q", s)
	}
}

func (s TaskScope) String() string { return string(s) }

// TaskStatus tracks the task lifecycle.
// NOTE: This is NOT the workflow execution status. WorkflowStatus is the source
// of truth for execution state. TaskStatus only tracks intake lifecycle.
type TaskStatus string

const (
	StatusCreated    TaskStatus = "created"
	StatusAccepted   TaskStatus = "accepted"
	StatusInProgress TaskStatus = "in_progress"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
	StatusCancelled  TaskStatus = "cancelled"
)

var validTaskTransitions = map[TaskStatus][]TaskStatus{
	StatusCreated:    {StatusAccepted, StatusCancelled},
	StatusAccepted:   {StatusInProgress, StatusCancelled},
	StatusInProgress: {StatusCompleted, StatusFailed, StatusCancelled},
}

// TaskMetadata carries additional consumer context. Immutable after creation.
type TaskMetadata map[string]any

// Task is the aggregate root for governance task intake.
type Task struct {
	id           shared.TaskID
	parentTaskID *shared.TaskID
	taskType     TaskType
	title        string
	scope        TaskScope
	priority     shared.Priority
	riskLevel    shared.RiskLevel
	status       TaskStatus
	metadata     TaskMetadata
	createdAt    shared.Timestamp
	updatedAt    shared.Timestamp
}

func NewTask(
	id shared.TaskID,
	taskType TaskType,
	title string,
	scope TaskScope,
	priority shared.Priority,
	riskLevel shared.RiskLevel,
	parentTaskID *shared.TaskID,
	metadata TaskMetadata,
	now shared.Timestamp,
) (*Task, error) {
	if title == "" {
		return nil, errors.New("task title must not be empty")
	}
	if metadata == nil {
		metadata = TaskMetadata{}
	}
	return &Task{
		id:           id,
		parentTaskID: parentTaskID,
		taskType:     taskType,
		title:        title,
		scope:        scope,
		priority:     priority,
		riskLevel:    riskLevel,
		status:       StatusCreated,
		metadata:     metadata,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// Accessors
func (t *Task) ID() shared.TaskID           { return t.id }
func (t *Task) ParentTaskID() *shared.TaskID { return t.parentTaskID }
func (t *Task) Type() TaskType               { return t.taskType }
func (t *Task) Title() string                { return t.title }
func (t *Task) Scope() TaskScope             { return t.scope }
func (t *Task) Priority() shared.Priority    { return t.priority }
func (t *Task) RiskLevel() shared.RiskLevel  { return t.riskLevel }
func (t *Task) Status() TaskStatus           { return t.status }
func (t *Task) Metadata() TaskMetadata       { return t.metadata }
func (t *Task) CreatedAt() shared.Timestamp  { return t.createdAt }
func (t *Task) UpdatedAt() shared.Timestamp  { return t.updatedAt }

// Transitions
func (t *Task) Accept(now shared.Timestamp) error   { return t.transitionTo(StatusAccepted, now) }
func (t *Task) Start(now shared.Timestamp) error     { return t.transitionTo(StatusInProgress, now) }
func (t *Task) Complete(now shared.Timestamp) error   { return t.transitionTo(StatusCompleted, now) }
func (t *Task) Fail(now shared.Timestamp) error       { return t.transitionTo(StatusFailed, now) }
func (t *Task) Cancel(now shared.Timestamp) error     { return t.transitionTo(StatusCancelled, now) }

func (t *Task) transitionTo(target TaskStatus, now shared.Timestamp) error {
	allowed, ok := validTaskTransitions[t.status]
	if !ok {
		return fmt.Errorf("task in terminal status %q cannot transition", t.status)
	}
	for _, s := range allowed {
		if s == target {
			t.status = target
			t.updatedAt = now
			return nil
		}
	}
	return fmt.Errorf("invalid task transition: %s → %s", t.status, target)
}

// Reconstruct creates a Task from persisted data without validation.
// Used only by repository adapters.
func Reconstruct(
	id shared.TaskID,
	parentTaskID *shared.TaskID,
	taskType TaskType,
	title string,
	scope TaskScope,
	priority shared.Priority,
	riskLevel shared.RiskLevel,
	status TaskStatus,
	metadata TaskMetadata,
	createdAt shared.Timestamp,
	updatedAt shared.Timestamp,
) *Task {
	return &Task{
		id: id, parentTaskID: parentTaskID, taskType: taskType,
		title: title, scope: scope, priority: priority,
		riskLevel: riskLevel, status: status, metadata: metadata,
		createdAt: createdAt, updatedAt: updatedAt,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/task/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/task/
git commit -m "feat: add Task aggregate root with lifecycle transitions"
```

---

## Task 3: Workflow + Execution Domain (G2: B5+B6)

**Files:**
- Create: `internal/domain/workflow/status.go`
- Create: `internal/domain/workflow/transition.go`
- Create: `internal/domain/workflow/workflow_run.go`
- Create: `internal/domain/workflow/workflow_run_test.go`
- Create: `internal/domain/execution/attempt.go`
- Create: `internal/domain/execution/attempt_test.go`
- Create: `internal/domain/execution/execution_lease.go`
- Create: `internal/domain/execution/execution_lease_test.go`

This is the most complex domain task. Workflow has the transition table; Execution has lease budgets and failure telemetry.

- [ ] **Step 1: Implement WorkflowStatus enum and WorkflowTransition VO**

```go
// internal/domain/workflow/status.go
package workflow

import "fmt"

type WorkflowStatus string

const (
	StatusCreated          WorkflowStatus = "created"
	StatusRouted           WorkflowStatus = "routed"
	StatusPolicyChecked    WorkflowStatus = "policy_checked"
	StatusRunning          WorkflowStatus = "running"
	StatusAwaitingApproval WorkflowStatus = "awaiting_approval"
	StatusApproved         WorkflowStatus = "approved"
	StatusPaused           WorkflowStatus = "paused"
	StatusCompleted        WorkflowStatus = "completed"
	StatusFailed           WorkflowStatus = "failed"
	StatusKilled           WorkflowStatus = "killed"
)

func NewWorkflowStatus(s string) (WorkflowStatus, error) {
	switch WorkflowStatus(s) {
	case StatusCreated, StatusRouted, StatusPolicyChecked, StatusRunning,
		StatusAwaitingApproval, StatusApproved, StatusPaused,
		StatusCompleted, StatusFailed, StatusKilled:
		return WorkflowStatus(s), nil
	default:
		return "", fmt.Errorf("invalid workflow status: %q", s)
	}
}

func (s WorkflowStatus) String() string { return string(s) }

func (s WorkflowStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusKilled
}

// validTransitions defines the state machine. Kill is handled separately.
var validTransitions = map[WorkflowStatus][]WorkflowStatus{
	StatusCreated:          {StatusRouted},
	StatusRouted:           {StatusPolicyChecked},
	StatusPolicyChecked:    {StatusRunning, StatusAwaitingApproval, StatusFailed},
	StatusAwaitingApproval: {StatusApproved, StatusFailed},
	StatusApproved:         {StatusRunning},
	StatusRunning:          {StatusCompleted, StatusFailed, StatusPaused, StatusKilled},
	StatusPaused:           {StatusRunning, StatusKilled},
}

func IsValidTransition(from, to WorkflowStatus) bool {
	if to == StatusKilled && !from.IsTerminal() {
		return true
	}
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
```

```go
// internal/domain/workflow/transition.go
package workflow

import "github.com/russellcxl/agent-governance-core/internal/domain/shared"

// WorkflowTransition records a single state change. Immutable.
type WorkflowTransition struct {
	From      WorkflowStatus
	To        WorkflowStatus
	Reason    string
	Actor     shared.ActorID
	Timestamp shared.Timestamp
}
```

- [ ] **Step 2: Write failing tests for WorkflowRun transitions**

```go
// internal/domain/workflow/workflow_run_test.go
package workflow_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testActor = shared.ActorID("system")
	testNow   = shared.MustTimestamp(time.Now())
)

func newTestWorkflowRun(t *testing.T) *workflow.WorkflowRun {
	t.Helper()
	wf, err := workflow.NewWorkflowRun(
		shared.WorkflowRunID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		testNow,
	)
	require.NoError(t, err)
	return wf
}

func TestNewWorkflowRun_StartsCreated(t *testing.T) {
	wf := newTestWorkflowRun(t)
	assert.Equal(t, workflow.StatusCreated, wf.Status())
	assert.Empty(t, wf.Transitions())
}

func TestWorkflowRun_ValidTransition_CreatedToRouted(t *testing.T) {
	wf := newTestWorkflowRun(t)
	rdID := shared.RoutingDecisionID("01HZXK5V6R3QW0F8YJ9N2TMGC3")
	err := wf.MarkRouted(rdID, "routed via scoring", testActor, testNow)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusRouted, wf.Status())
	assert.Len(t, wf.Transitions(), 1)
	assert.Equal(t, &rdID, wf.RoutingDecisionID())
}

func TestWorkflowRun_InvalidTransition_CreatedToRunning(t *testing.T) {
	wf := newTestWorkflowRun(t)
	err := wf.TransitionTo(workflow.StatusRunning, "skip", testActor, testNow)
	assert.Error(t, err)
	assert.Equal(t, workflow.StatusCreated, wf.Status()) // unchanged
}

func TestWorkflowRun_Kill_FromRunning(t *testing.T) {
	wf := runningWorkflow(t)
	err := wf.Kill("emergency stop", testActor, testNow)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusKilled, wf.Status())
	assert.True(t, wf.Status().IsTerminal())
}

func TestWorkflowRun_Kill_FromPaused(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Pause("need review", testActor, testNow))
	err := wf.Kill("emergency", testActor, testNow)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusKilled, wf.Status())
}

func TestWorkflowRun_Kill_FromTerminal_Fails(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Complete("done", testActor, testNow))
	err := wf.Kill("too late", testActor, testNow)
	assert.Error(t, err)
}

func TestWorkflowRun_NoTransitionFromTerminal(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Kill("stop", testActor, testNow))
	err := wf.TransitionTo(workflow.StatusRunning, "resume?", testActor, testNow)
	assert.Error(t, err)
}

func TestWorkflowRun_CannotBeRunningAndAwaitingApproval(t *testing.T) {
	wf := newTestWorkflowRun(t)
	rdID := shared.RoutingDecisionID("01HZXK5V6R3QW0F8YJ9N2TMGC3")
	pdID := shared.PolicyDecisionID("01HZXK5V6R3QW0F8YJ9N2TMGC4")
	require.NoError(t, wf.MarkRouted(rdID, "routed", testActor, testNow))
	require.NoError(t, wf.MarkPolicyChecked(pdID, testActor, testNow))
	require.NoError(t, wf.TransitionTo(workflow.StatusAwaitingApproval, "needs approval", testActor, testNow))
	// Cannot go to running without going through approved first
	err := wf.TransitionTo(workflow.StatusRunning, "skip approval", testActor, testNow)
	assert.Error(t, err)
}

// runningWorkflow creates a WorkflowRun that has gone through the full happy path to running.
func runningWorkflow(t *testing.T) *workflow.WorkflowRun {
	t.Helper()
	wf := newTestWorkflowRun(t)
	rdID := shared.RoutingDecisionID("01HZXK5V6R3QW0F8YJ9N2TMGC3")
	pdID := shared.PolicyDecisionID("01HZXK5V6R3QW0F8YJ9N2TMGC4")
	require.NoError(t, wf.MarkRouted(rdID, "routed", testActor, testNow))
	require.NoError(t, wf.MarkPolicyChecked(pdID, testActor, testNow))
	require.NoError(t, wf.TransitionTo(workflow.StatusRunning, "policy allows", testActor, testNow))
	return wf
}
```

- [ ] **Step 3: Implement WorkflowRun aggregate**

```go
// internal/domain/workflow/workflow_run.go
package workflow

import (
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type WorkflowRun struct {
	id                shared.WorkflowRunID
	taskID            shared.TaskID
	status            WorkflowStatus
	routingDecisionID *shared.RoutingDecisionID
	policyDecisionID  *shared.PolicyDecisionID
	currentStepIndex  int
	transitions       []WorkflowTransition
	createdAt         shared.Timestamp
	updatedAt         shared.Timestamp
}

func NewWorkflowRun(id shared.WorkflowRunID, taskID shared.TaskID, now shared.Timestamp) (*WorkflowRun, error) {
	return &WorkflowRun{
		id:        id,
		taskID:    taskID,
		status:    StatusCreated,
		createdAt: now,
		updatedAt: now,
	}, nil
}

// Accessors
func (w *WorkflowRun) ID() shared.WorkflowRunID                { return w.id }
func (w *WorkflowRun) TaskID() shared.TaskID                    { return w.taskID }
func (w *WorkflowRun) Status() WorkflowStatus                  { return w.status }
func (w *WorkflowRun) RoutingDecisionID() *shared.RoutingDecisionID { return w.routingDecisionID }
func (w *WorkflowRun) PolicyDecisionID() *shared.PolicyDecisionID   { return w.policyDecisionID }
func (w *WorkflowRun) CurrentStepIndex() int                    { return w.currentStepIndex }
func (w *WorkflowRun) Transitions() []WorkflowTransition        { return w.transitions }
func (w *WorkflowRun) CreatedAt() shared.Timestamp              { return w.createdAt }
func (w *WorkflowRun) UpdatedAt() shared.Timestamp              { return w.updatedAt }

func (w *WorkflowRun) MarkRouted(rdID shared.RoutingDecisionID, reason string, actor shared.ActorID, now shared.Timestamp) error {
	if err := w.TransitionTo(StatusRouted, reason, actor, now); err != nil {
		return err
	}
	w.routingDecisionID = &rdID
	return nil
}

func (w *WorkflowRun) MarkPolicyChecked(pdID shared.PolicyDecisionID, actor shared.ActorID, now shared.Timestamp) error {
	if err := w.TransitionTo(StatusPolicyChecked, "policy evaluated", actor, now); err != nil {
		return err
	}
	w.policyDecisionID = &pdID
	return nil
}

func (w *WorkflowRun) Pause(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.TransitionTo(StatusPaused, reason, actor, now)
}

func (w *WorkflowRun) Resume(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.TransitionTo(StatusRunning, reason, actor, now)
}

func (w *WorkflowRun) Complete(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.TransitionTo(StatusCompleted, reason, actor, now)
}

func (w *WorkflowRun) Fail(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.TransitionTo(StatusFailed, reason, actor, now)
}

func (w *WorkflowRun) Kill(reason string, actor shared.ActorID, now shared.Timestamp) error {
	if w.status.IsTerminal() {
		return fmt.Errorf("cannot kill workflow in terminal status %q", w.status)
	}
	return w.transitionUnchecked(StatusKilled, reason, actor, now)
}

func (w *WorkflowRun) TransitionTo(target WorkflowStatus, reason string, actor shared.ActorID, now shared.Timestamp) error {
	if !IsValidTransition(w.status, target) {
		return fmt.Errorf("invalid workflow transition: %s → %s", w.status, target)
	}
	return w.transitionUnchecked(target, reason, actor, now)
}

func (w *WorkflowRun) transitionUnchecked(target WorkflowStatus, reason string, actor shared.ActorID, now shared.Timestamp) error {
	w.transitions = append(w.transitions, WorkflowTransition{
		From:      w.status,
		To:        target,
		Reason:    reason,
		Actor:     actor,
		Timestamp: now,
	})
	w.status = target
	w.updatedAt = now
	return nil
}

// ReconstructWorkflowRun creates a WorkflowRun from persisted data.
func ReconstructWorkflowRun(
	id shared.WorkflowRunID,
	taskID shared.TaskID,
	status WorkflowStatus,
	routingDecisionID *shared.RoutingDecisionID,
	policyDecisionID *shared.PolicyDecisionID,
	currentStepIndex int,
	transitions []WorkflowTransition,
	createdAt shared.Timestamp,
	updatedAt shared.Timestamp,
) *WorkflowRun {
	return &WorkflowRun{
		id: id, taskID: taskID, status: status,
		routingDecisionID: routingDecisionID, policyDecisionID: policyDecisionID,
		currentStepIndex: currentStepIndex, transitions: transitions,
		createdAt: createdAt, updatedAt: updatedAt,
	}
}
```

- [ ] **Step 4: Run workflow tests**

Run: `go test ./internal/domain/workflow/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Implement AttemptResult with failure telemetry**

```go
// internal/domain/execution/attempt.go
package execution

import (
	"errors"
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type AttemptStatus string

const (
	AttemptSuccess AttemptStatus = "success"
	AttemptFailure AttemptStatus = "failure"
	AttemptRetry   AttemptStatus = "retry"
)

func NewAttemptStatus(s string) (AttemptStatus, error) {
	switch AttemptStatus(s) {
	case AttemptSuccess, AttemptFailure, AttemptRetry:
		return AttemptStatus(s), nil
	default:
		return "", fmt.Errorf("invalid attempt status: %q", s)
	}
}

// AttemptResult captures the outcome of an execution attempt with failure telemetry.
type AttemptResult struct {
	Status       AttemptStatus
	FailureStage *shared.FailureStage
	FailureCode  *string
	Retryable    *bool
	ToolName     *string
	StrategyUsed *string
	AgentRole    *string
	Detail       *string
}

func NewSuccessResult() AttemptResult {
	return AttemptResult{Status: AttemptSuccess}
}

func NewFailureResult(stage shared.FailureStage, code string, retryable bool) (AttemptResult, error) {
	if code == "" {
		return AttemptResult{}, errors.New("failure_code is required for failure results")
	}
	return AttemptResult{
		Status:       AttemptFailure,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
	}, nil
}

func NewRetryResult(stage shared.FailureStage, code string) (AttemptResult, error) {
	if code == "" {
		return AttemptResult{}, errors.New("failure_code is required for retry results")
	}
	retryable := true
	return AttemptResult{
		Status:       AttemptRetry,
		FailureStage: &stage,
		FailureCode:  &code,
		Retryable:    &retryable,
	}, nil
}

func (r AttemptResult) WithToolName(name string) AttemptResult     { r.ToolName = &name; return r }
func (r AttemptResult) WithStrategy(s string) AttemptResult        { r.StrategyUsed = &s; return r }
func (r AttemptResult) WithAgentRole(role string) AttemptResult    { r.AgentRole = &role; return r }
func (r AttemptResult) WithDetail(detail string) AttemptResult     { r.Detail = &detail; return r }

func (r AttemptResult) Validate() error {
	if r.Status == AttemptSuccess {
		if r.FailureStage != nil || r.FailureCode != nil || r.Retryable != nil {
			return errors.New("success result must not have failure fields")
		}
		return nil
	}
	// failure or retry
	if r.FailureStage == nil {
		return errors.New("failure_stage is required for failure/retry results")
	}
	if r.FailureCode == nil {
		return errors.New("failure_code is required for failure/retry results")
	}
	if r.Retryable == nil {
		return errors.New("retryable is required for failure/retry results")
	}
	return nil
}
```

- [ ] **Step 6: Write and run AttemptResult tests**

```go
// internal/domain/execution/attempt_test.go
package execution_test

import (
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttemptResult_Success_Valid(t *testing.T) {
	r := execution.NewSuccessResult()
	assert.NoError(t, r.Validate())
	assert.Equal(t, execution.AttemptSuccess, r.Status)
}

func TestAttemptResult_Success_WithFailureFields_Invalid(t *testing.T) {
	stage := shared.StageRuntime
	r := execution.AttemptResult{
		Status:       execution.AttemptSuccess,
		FailureStage: &stage,
	}
	assert.Error(t, r.Validate())
}

func TestAttemptResult_Failure_Valid(t *testing.T) {
	r, err := execution.NewFailureResult(shared.StageRuntime, "runtime/oom", false)
	require.NoError(t, err)
	assert.NoError(t, r.Validate())
	assert.Equal(t, execution.AttemptFailure, r.Status)
}

func TestAttemptResult_Failure_MissingCode(t *testing.T) {
	_, err := execution.NewFailureResult(shared.StageRuntime, "", false)
	assert.Error(t, err)
}

func TestAttemptResult_WithOptionalFields(t *testing.T) {
	r, _ := execution.NewFailureResult(shared.StageRuntime, "tool/shell_timeout", true)
	r = r.WithToolName("shell").WithStrategy("direct").WithAgentRole("implementer")
	assert.Equal(t, "shell", *r.ToolName)
	assert.Equal(t, "direct", *r.StrategyUsed)
}
```

Run: `go test ./internal/domain/execution/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 7: Implement ExecutionLease aggregate**

```go
// internal/domain/execution/execution_lease.go
package execution

import (
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type LeaseStatus string

const (
	LeaseActive    LeaseStatus = "active"
	LeaseExhausted LeaseStatus = "exhausted"
	LeaseRevoked   LeaseStatus = "revoked"
)

type RetryBudget struct {
	MaxAttempts int
}

func NewRetryBudget(max int) (RetryBudget, error) {
	if max <= 0 {
		return RetryBudget{}, fmt.Errorf("retry budget must be > 0, got %d", max)
	}
	return RetryBudget{MaxAttempts: max}, nil
}

type ExecutionLease struct {
	id             shared.ExecutionLeaseID
	workflowRunID  shared.WorkflowRunID
	timeoutBudget  time.Duration
	retryBudget    RetryBudget
	attemptsUsed   int
	timeElapsed    time.Duration
	status         LeaseStatus
	createdAt      shared.Timestamp
}

func NewExecutionLease(
	id shared.ExecutionLeaseID,
	workflowRunID shared.WorkflowRunID,
	timeoutBudget time.Duration,
	retryBudget RetryBudget,
	now shared.Timestamp,
) (*ExecutionLease, error) {
	if timeoutBudget <= 0 {
		return nil, fmt.Errorf("timeout budget must be > 0")
	}
	return &ExecutionLease{
		id:            id,
		workflowRunID: workflowRunID,
		timeoutBudget: timeoutBudget,
		retryBudget:   retryBudget,
		status:        LeaseActive,
		createdAt:     now,
	}, nil
}

func (l *ExecutionLease) ID() shared.ExecutionLeaseID     { return l.id }
func (l *ExecutionLease) WorkflowRunID() shared.WorkflowRunID { return l.workflowRunID }
func (l *ExecutionLease) TimeoutBudget() time.Duration    { return l.timeoutBudget }
func (l *ExecutionLease) RetryBudget() RetryBudget        { return l.retryBudget }
func (l *ExecutionLease) AttemptsUsed() int               { return l.attemptsUsed }
func (l *ExecutionLease) TimeElapsed() time.Duration      { return l.timeElapsed }
func (l *ExecutionLease) Status() LeaseStatus             { return l.status }
func (l *ExecutionLease) CreatedAt() shared.Timestamp     { return l.createdAt }

func (l *ExecutionLease) HasRetryBudget() bool {
	return l.attemptsUsed < l.retryBudget.MaxAttempts
}

func (l *ExecutionLease) RecordAttempt() error {
	if l.status != LeaseActive {
		return fmt.Errorf("cannot record attempt on %s lease", l.status)
	}
	l.attemptsUsed++
	if l.attemptsUsed >= l.retryBudget.MaxAttempts {
		l.status = LeaseExhausted
	}
	return nil
}

func (l *ExecutionLease) UpdateTimeElapsed(elapsed time.Duration) {
	l.timeElapsed = elapsed
}

func (l *ExecutionLease) Revoke() error {
	if l.status != LeaseActive {
		return fmt.Errorf("cannot revoke %s lease", l.status)
	}
	l.status = LeaseRevoked
	return nil
}

func ReconstructExecutionLease(
	id shared.ExecutionLeaseID,
	workflowRunID shared.WorkflowRunID,
	timeoutBudget time.Duration,
	retryBudget RetryBudget,
	attemptsUsed int,
	timeElapsed time.Duration,
	status LeaseStatus,
	createdAt shared.Timestamp,
) *ExecutionLease {
	return &ExecutionLease{
		id: id, workflowRunID: workflowRunID, timeoutBudget: timeoutBudget,
		retryBudget: retryBudget, attemptsUsed: attemptsUsed,
		timeElapsed: timeElapsed, status: status, createdAt: createdAt,
	}
}
```

- [ ] **Step 8: Write and run ExecutionLease tests**

```go
// internal/domain/execution/execution_lease_test.go
package execution_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLease(t *testing.T, maxAttempts int) *execution.ExecutionLease {
	t.Helper()
	budget, err := execution.NewRetryBudget(maxAttempts)
	require.NoError(t, err)
	lease, err := execution.NewExecutionLease(
		shared.ExecutionLeaseID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.WorkflowRunID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		5*time.Minute,
		budget,
		shared.MustTimestamp(time.Now()),
	)
	require.NoError(t, err)
	return lease
}

func TestExecutionLease_RecordAttempt(t *testing.T) {
	lease := newTestLease(t, 3)
	require.NoError(t, lease.RecordAttempt())
	assert.Equal(t, 1, lease.AttemptsUsed())
	assert.Equal(t, execution.LeaseActive, lease.Status())
}

func TestExecutionLease_BudgetExhausted(t *testing.T) {
	lease := newTestLease(t, 2)
	require.NoError(t, lease.RecordAttempt())
	require.NoError(t, lease.RecordAttempt())
	assert.Equal(t, execution.LeaseExhausted, lease.Status())
	assert.False(t, lease.HasRetryBudget())
}

func TestExecutionLease_AttemptOnExhausted_Fails(t *testing.T) {
	lease := newTestLease(t, 1)
	require.NoError(t, lease.RecordAttempt())
	err := lease.RecordAttempt()
	assert.Error(t, err)
}

func TestExecutionLease_Revoke(t *testing.T) {
	lease := newTestLease(t, 3)
	require.NoError(t, lease.Revoke())
	assert.Equal(t, execution.LeaseRevoked, lease.Status())
}

func TestExecutionLease_AttemptOnRevoked_Fails(t *testing.T) {
	lease := newTestLease(t, 3)
	require.NoError(t, lease.Revoke())
	err := lease.RecordAttempt()
	assert.Error(t, err)
}

func TestRetryBudget_ZeroOrNegative_Fails(t *testing.T) {
	_, err := execution.NewRetryBudget(0)
	assert.Error(t, err)
	_, err = execution.NewRetryBudget(-1)
	assert.Error(t, err)
}
```

Run: `go test ./internal/domain/execution/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 9: Commit**

```bash
git add internal/domain/workflow/ internal/domain/execution/
git commit -m "feat: add WorkflowRun state machine and ExecutionLease with failure telemetry"
```

---

## Task 4: Routing Domain (G2: B3)

**Files:**
- Create: `internal/domain/routing/routing_decision.go`
- Create: `internal/domain/routing/strategy.go`
- Create: `internal/domain/routing/rules.go`
- Create: `internal/domain/routing/evaluator.go`
- Create: `internal/domain/routing/evaluator_test.go`

- [ ] **Step 1: Implement routing VOs and RoutingDecision aggregate**

```go
// internal/domain/routing/strategy.go
package routing

import "fmt"

type RoutingStrategy string

const (
	StrategyDirect      RoutingStrategy = "direct"
	StrategyDecompose   RoutingStrategy = "decompose"
	StrategyCollaborate RoutingStrategy = "collaborate" // Phase 2
	StrategyEscalate    RoutingStrategy = "escalate"
)

func NewRoutingStrategy(s string) (RoutingStrategy, error) {
	switch RoutingStrategy(s) {
	case StrategyDirect, StrategyDecompose, StrategyCollaborate, StrategyEscalate:
		return RoutingStrategy(s), nil
	default:
		return "", fmt.Errorf("invalid routing strategy: %q", s)
	}
}

func (s RoutingStrategy) String() string { return string(s) }

type AgentRole string

const (
	RoleImplementer AgentRole = "implementer"
	RoleReviewer    AgentRole = "reviewer"
	RoleResearcher  AgentRole = "researcher"
	RoleArchitect   AgentRole = "architect"
	RoleHuman       AgentRole = "human"
)

func NewAgentRole(s string) (AgentRole, error) {
	switch AgentRole(s) {
	case RoleImplementer, RoleReviewer, RoleResearcher, RoleArchitect, RoleHuman:
		return AgentRole(s), nil
	default:
		return "", fmt.Errorf("invalid agent role: %q", s)
	}
}

type StrategyEvaluation struct {
	Strategy     RoutingStrategy
	Score        float64
	FactorScores map[string]float64
	Overridden   bool
	Reason       string
}

type RoutingConstraint struct {
	Type  string
	Value string
}
```

```go
// internal/domain/routing/routing_decision.go
package routing

import (
	"errors"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type RoutingDecision struct {
	id                  shared.RoutingDecisionID
	taskID              shared.TaskID
	evaluatedStrategies []StrategyEvaluation
	selectedStrategy    RoutingStrategy
	selectedAgentRole   AgentRole
	reason              string
	constraints         []RoutingConstraint
	createdAt           shared.Timestamp
}

func NewRoutingDecision(
	id shared.RoutingDecisionID,
	taskID shared.TaskID,
	evaluatedStrategies []StrategyEvaluation,
	selectedStrategy RoutingStrategy,
	selectedAgentRole AgentRole,
	reason string,
	constraints []RoutingConstraint,
	now shared.Timestamp,
) (*RoutingDecision, error) {
	if len(evaluatedStrategies) == 0 {
		return nil, errors.New("must have at least one evaluated strategy")
	}
	if reason == "" {
		return nil, errors.New("routing decision must have a reason")
	}
	return &RoutingDecision{
		id: id, taskID: taskID, evaluatedStrategies: evaluatedStrategies,
		selectedStrategy: selectedStrategy, selectedAgentRole: selectedAgentRole,
		reason: reason, constraints: constraints, createdAt: now,
	}, nil
}

func (d *RoutingDecision) ID() shared.RoutingDecisionID        { return d.id }
func (d *RoutingDecision) TaskID() shared.TaskID               { return d.taskID }
func (d *RoutingDecision) EvaluatedStrategies() []StrategyEvaluation { return d.evaluatedStrategies }
func (d *RoutingDecision) SelectedStrategy() RoutingStrategy    { return d.selectedStrategy }
func (d *RoutingDecision) SelectedAgentRole() AgentRole         { return d.selectedAgentRole }
func (d *RoutingDecision) Reason() string                       { return d.reason }
func (d *RoutingDecision) Constraints() []RoutingConstraint     { return d.constraints }
func (d *RoutingDecision) CreatedAt() shared.Timestamp          { return d.createdAt }

func ReconstructRoutingDecision(
	id shared.RoutingDecisionID, taskID shared.TaskID,
	evaluatedStrategies []StrategyEvaluation, selectedStrategy RoutingStrategy,
	selectedAgentRole AgentRole, reason string, constraints []RoutingConstraint,
	createdAt shared.Timestamp,
) *RoutingDecision {
	return &RoutingDecision{
		id: id, taskID: taskID, evaluatedStrategies: evaluatedStrategies,
		selectedStrategy: selectedStrategy, selectedAgentRole: selectedAgentRole,
		reason: reason, constraints: constraints, createdAt: createdAt,
	}
}
```

- [ ] **Step 2: Implement routing evaluator with overrides, scoring, tiebreaker**

```go
// internal/domain/routing/rules.go
package routing

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

// MemoryContext holds optional context from memory-engine for routing decisions.
type MemoryContext struct {
	SimilarTaskFailureRate float64
	HasRelevantHeuristics  bool
}

// Factor weights for score calculation. Sum = 1.0.
const (
	WeightRiskLevel        = 0.30
	WeightTaskScope        = 0.25
	WeightTaskType         = 0.20
	WeightTaskPriority     = 0.10
	WeightSimilarTasks     = 0.10
	WeightHeuristics       = 0.05
)

func scoreRiskLevel(risk shared.RiskLevel, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyEscalate:
		switch risk {
		case shared.RiskCritical:
			return 1.0
		case shared.RiskHigh:
			return 0.7
		default:
			return 0.2
		}
	case StrategyDirect:
		switch risk {
		case shared.RiskLow:
			return 0.9
		case shared.RiskMedium:
			return 0.6
		default:
			return 0.1
		}
	case StrategyDecompose:
		switch risk {
		case shared.RiskMedium, shared.RiskHigh:
			return 0.6
		default:
			return 0.3
		}
	}
	return 0.0
}

func scoreTaskScope(scope task.TaskScope, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		if scope == task.ScopeFile {
			return 0.9
		}
		if scope == task.ScopeModule {
			return 0.6
		}
		return 0.2
	case StrategyDecompose:
		if scope == task.ScopeRepo || scope == task.ScopeSystem {
			return 0.8
		}
		return 0.3
	case StrategyEscalate:
		if scope == task.ScopeSystem {
			return 0.8
		}
		return 0.3
	}
	return 0.0
}

func scoreTaskType(tt task.TaskType, strategy RoutingStrategy) float64 {
	switch strategy {
	case StrategyDirect:
		if tt == task.TypeBugfix || tt == task.TypeReview {
			return 0.8
		}
		return 0.5
	case StrategyDecompose:
		if tt == task.TypeRefactor || tt == task.TypeDevelopment {
			return 0.7
		}
		return 0.3
	case StrategyEscalate:
		if tt == task.TypeDeployment {
			return 0.9
		}
		return 0.2
	}
	return 0.0
}

func scoreTaskPriority(p shared.Priority, strategy RoutingStrategy) float64 {
	if strategy == StrategyEscalate && p == shared.PriorityUrgent {
		return 0.8
	}
	return 0.5
}

func scoreMemory(mc *MemoryContext, strategy RoutingStrategy) (similarScore, heuristicScore float64) {
	if mc == nil {
		return 0.5, 0.5 // neutral when no memory available
	}
	if strategy == StrategyEscalate && mc.SimilarTaskFailureRate > 0.5 {
		similarScore = 0.8
	} else {
		similarScore = 0.5
	}
	if mc.HasRelevantHeuristics {
		heuristicScore = 0.7
	} else {
		heuristicScore = 0.5
	}
	return
}
```

```go
// internal/domain/routing/evaluator.go
package routing

import (
	"fmt"
	"sort"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

type EvaluatorInput struct {
	Task          *task.Task
	MemoryContext *MemoryContext
}

type EvaluatorResult struct {
	Evaluations      []StrategyEvaluation
	SelectedStrategy RoutingStrategy
	SelectedRole     AgentRole
	Reason           string
}

// Evaluate runs hard overrides first, then scoring with tiebreaker.
func Evaluate(input EvaluatorInput) EvaluatorResult {
	// 1. Hard overrides
	if override, reason, ok := checkOverrides(input); ok {
		role := defaultRoleForStrategy(override)
		eval := StrategyEvaluation{
			Strategy:   override,
			Score:      1.0,
			Overridden: true,
			Reason:     reason,
		}
		return EvaluatorResult{
			Evaluations:      []StrategyEvaluation{eval},
			SelectedStrategy: override,
			SelectedRole:     role,
			Reason:           fmt.Sprintf("[override] %s", reason),
		}
	}

	// 2. Score-based evaluation
	strategies := []RoutingStrategy{StrategyDirect, StrategyDecompose, StrategyEscalate}
	evals := make([]StrategyEvaluation, 0, len(strategies))

	for _, s := range strategies {
		factorScores := map[string]float64{
			"risk_level":    scoreRiskLevel(input.Task.RiskLevel(), s),
			"task_scope":    scoreTaskScope(input.Task.Scope(), s),
			"task_type":     scoreTaskType(input.Task.Type(), s),
			"task_priority": scoreTaskPriority(input.Task.Priority(), s),
		}
		simScore, heurScore := scoreMemory(input.MemoryContext, s)
		factorScores["memory_similar_tasks"] = simScore
		factorScores["memory_heuristics"] = heurScore

		total := factorScores["risk_level"]*WeightRiskLevel +
			factorScores["task_scope"]*WeightTaskScope +
			factorScores["task_type"]*WeightTaskType +
			factorScores["task_priority"]*WeightTaskPriority +
			factorScores["memory_similar_tasks"]*WeightSimilarTasks +
			factorScores["memory_heuristics"]*WeightHeuristics

		evals = append(evals, StrategyEvaluation{
			Strategy:     s,
			Score:        total,
			FactorScores: factorScores,
			Reason:       fmt.Sprintf("score=%.3f", total),
		})
	}

	// 3. Select highest score with tiebreaker
	sort.Slice(evals, func(i, j int) bool {
		if evals[i].Score == evals[j].Score {
			return strategyPriority(evals[i].Strategy) < strategyPriority(evals[j].Strategy)
		}
		return evals[i].Score > evals[j].Score
	})

	selected := evals[0]
	role := defaultRoleForStrategy(selected.Strategy)

	return EvaluatorResult{
		Evaluations:      evals,
		SelectedStrategy: selected.Strategy,
		SelectedRole:     role,
		Reason:           fmt.Sprintf("selected %s: %s", selected.Strategy, selected.Reason),
	}
}

func checkOverrides(input EvaluatorInput) (RoutingStrategy, string, bool) {
	if input.Task.RiskLevel() == shared.RiskCritical {
		return StrategyEscalate, "risk_level is critical", true
	}
	if input.Task.Scope() == task.ScopeSystem && input.Task.Type() == task.TypeDeployment {
		return StrategyEscalate, "system-scope deployment", true
	}
	if fs, ok := input.Task.Metadata()["force_strategy"]; ok {
		if s, err := NewRoutingStrategy(fs.(string)); err == nil {
			return s, fmt.Sprintf("consumer override: force_strategy=%s", s), true
		}
	}
	return "", "", false
}

// strategyPriority: lower number = preferred in tiebreaker. direct > decompose > escalate.
func strategyPriority(s RoutingStrategy) int {
	switch s {
	case StrategyDirect:
		return 0
	case StrategyDecompose:
		return 1
	case StrategyEscalate:
		return 2
	default:
		return 99
	}
}

func defaultRoleForStrategy(s RoutingStrategy) AgentRole {
	switch s {
	case StrategyEscalate:
		return RoleHuman
	case StrategyDecompose:
		return RoleArchitect
	default:
		return RoleImplementer
	}
}
```

- [ ] **Step 3: Write and run evaluator tests**

```go
// internal/domain/routing/evaluator_test.go
package routing_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTask(t *testing.T, tt task.TaskType, scope task.TaskScope, risk shared.RiskLevel, meta task.TaskMetadata) *task.Task {
	t.Helper()
	now := shared.MustTimestamp(time.Now())
	tk, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"), tt, "test", scope,
		shared.PriorityNormal, risk, nil, meta, now,
	)
	require.NoError(t, err)
	return tk
}

func TestEvaluator_Override_CriticalRisk(t *testing.T) {
	tk := makeTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskCritical, nil)
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	assert.Equal(t, routing.StrategyEscalate, result.SelectedStrategy)
	assert.Contains(t, result.Reason, "[override]")
}

func TestEvaluator_Override_SystemDeployment(t *testing.T) {
	tk := makeTask(t, task.TypeDeployment, task.ScopeSystem, shared.RiskHigh, nil)
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	assert.Equal(t, routing.StrategyEscalate, result.SelectedStrategy)
	assert.Contains(t, result.Reason, "[override]")
}

func TestEvaluator_Override_ForceStrategy(t *testing.T) {
	tk := makeTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskLow,
		task.TaskMetadata{"force_strategy": "decompose"})
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	assert.Equal(t, routing.StrategyDecompose, result.SelectedStrategy)
	assert.Contains(t, result.Reason, "[override]")
}

func TestEvaluator_Scoring_SimpleBugfix(t *testing.T) {
	tk := makeTask(t, task.TypeBugfix, task.ScopeFile, shared.RiskLow, nil)
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	assert.Equal(t, routing.StrategyDirect, result.SelectedStrategy)
	assert.Len(t, result.Evaluations, 3) // direct, decompose, escalate
}

func TestEvaluator_Scoring_LargeRefactor(t *testing.T) {
	tk := makeTask(t, task.TypeRefactor, task.ScopeRepo, shared.RiskMedium, nil)
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	assert.Equal(t, routing.StrategyDecompose, result.SelectedStrategy)
}

func TestEvaluator_Tiebreaker_FavorsSimplest(t *testing.T) {
	// Tie should favor direct over decompose over escalate
	// This test verifies the tiebreaker logic exists; actual ties are rare with
	// continuous scores, but the logic must be correct.
	tk := makeTask(t, task.TypeResearch, task.ScopeModule, shared.RiskMedium, nil)
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk})
	// Research/module/medium doesn't strongly favor any strategy, but tiebreaker
	// should prefer direct or decompose over escalate for same scores
	assert.NotEqual(t, routing.StrategyEscalate, result.SelectedStrategy)
}

func TestEvaluator_WithMemoryContext(t *testing.T) {
	tk := makeTask(t, task.TypeDevelopment, task.ScopeModule, shared.RiskMedium, nil)
	mc := &routing.MemoryContext{SimilarTaskFailureRate: 0.8, HasRelevantHeuristics: true}
	result := routing.Evaluate(routing.EvaluatorInput{Task: tk, MemoryContext: mc})
	// High failure rate should increase escalate score
	assert.NotEmpty(t, result.Evaluations)
}
```

Run: `go test ./internal/domain/routing/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/domain/routing/
git commit -m "feat: add routing domain — RoutingDecision, score evaluator with overrides and tiebreaker"
```

---

## Task 5: Policy Domain (G2: B4)

**Files:**
- Create: `internal/domain/policy/outcome.go`
- Create: `internal/domain/policy/rule.go`
- Create: `internal/domain/policy/rules.go`
- Create: `internal/domain/policy/sensitivity.go`
- Create: `internal/domain/policy/policy_decision.go`
- Create: `internal/domain/policy/evaluator.go`
- Create: `internal/domain/policy/evaluator_test.go`

This task follows the same pattern as routing. The key difference: policy evaluates sequentially and most restrictive wins.

- [ ] **Step 1: Implement policy VOs, PolicyRule interface, and concrete rules**

```go
// internal/domain/policy/outcome.go
package policy

import "fmt"

type PolicyOutcome string

const (
	OutcomeAllow                PolicyOutcome = "allow"
	OutcomeAllowWithConstraints PolicyOutcome = "allow_with_constraints"
	OutcomeRequireApproval      PolicyOutcome = "require_approval"
	OutcomeDeny                 PolicyOutcome = "deny"
)

func NewPolicyOutcome(s string) (PolicyOutcome, error) {
	switch PolicyOutcome(s) {
	case OutcomeAllow, OutcomeAllowWithConstraints, OutcomeRequireApproval, OutcomeDeny:
		return PolicyOutcome(s), nil
	default:
		return "", fmt.Errorf("invalid policy outcome: %q", s)
	}
}

func (o PolicyOutcome) String() string { return string(o) }

// Restrictiveness returns a numeric value. Higher = more restrictive.
func (o PolicyOutcome) Restrictiveness() int {
	switch o {
	case OutcomeAllow:
		return 0
	case OutcomeAllowWithConstraints:
		return 1
	case OutcomeRequireApproval:
		return 2
	case OutcomeDeny:
		return 3
	default:
		return -1
	}
}

type PolicyConstraint struct {
	Type   string
	Value  string
	Reason string
}

type ApprovalRequirement struct {
	ApproverRole string
	Reason       string
	TimeoutSecs  *int
}

type RuleEvaluation struct {
	RuleID  string
	Passed  bool
	Outcome *PolicyOutcome // nil if rule didn't match
	Reason  string
}
```

```go
// internal/domain/policy/rule.go
package policy

import "github.com/russellcxl/agent-governance-core/internal/domain/task"

type PolicyContext struct {
	Task   *task.Task
	Action string
}

type PolicyRule interface {
	ID() string
	Evaluate(ctx PolicyContext) RuleEvaluation
}
```

```go
// internal/domain/policy/sensitivity.go
package policy

var sensitiveActions = map[string]bool{
	"shell_execution":    true,
	"git_push":           true,
	"external_api_call":  true,
	"deployment":         true,
	"data_deletion":      true,
}

func IsActionSensitive(action string) bool {
	return sensitiveActions[action]
}
```

```go
// internal/domain/policy/rules.go
package policy

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

type riskCriticalRule struct{}

func (r riskCriticalRule) ID() string { return "risk_critical_requires_approval" }
func (r riskCriticalRule) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.RiskLevel() == shared.RiskCritical {
		o := OutcomeRequireApproval
		return RuleEvaluation{RuleID: r.ID(), Passed: false, Outcome: &o, Reason: "critical risk requires approval"}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Reason: "risk level is not critical"}
}

type deploymentRule struct{}

func (r deploymentRule) ID() string { return "deployment_requires_approval" }
func (r deploymentRule) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Type() == task.TypeDeployment {
		o := OutcomeRequireApproval
		return RuleEvaluation{RuleID: r.ID(), Passed: false, Outcome: &o, Reason: "deployments require approval"}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Reason: "not a deployment"}
}

type systemScopeRule struct{}

func (r systemScopeRule) ID() string { return "system_scope_requires_approval" }
func (r systemScopeRule) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Scope() == task.ScopeSystem {
		o := OutcomeRequireApproval
		return RuleEvaluation{RuleID: r.ID(), Passed: false, Outcome: &o, Reason: "system scope requires approval"}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Reason: "scope is not system"}
}

type destructiveActionRule struct{}

func (r destructiveActionRule) ID() string { return "destructive_action_deny" }
func (r destructiveActionRule) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Action == "data_deletion" {
		o := OutcomeDeny
		return RuleEvaluation{RuleID: r.ID(), Passed: false, Outcome: &o, Reason: "destructive action denied"}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Reason: "action is not destructive"}
}

type fileScopeLowRiskRule struct{}

func (r fileScopeLowRiskRule) ID() string { return "file_scope_low_risk_allow" }
func (r fileScopeLowRiskRule) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Scope() == task.ScopeFile && ctx.Task.RiskLevel() == shared.RiskLow {
		o := OutcomeAllow
		return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: &o, Reason: "low risk file scope allowed"}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Reason: "does not match file+low criteria"}
}

// DefaultRules returns the phase 1 policy rule set in evaluation order.
func DefaultRules() []PolicyRule {
	return []PolicyRule{
		riskCriticalRule{},
		deploymentRule{},
		systemScopeRule{},
		destructiveActionRule{},
		fileScopeLowRiskRule{},
	}
}
```

- [ ] **Step 2: Implement PolicyDecision aggregate and PolicyEvaluator**

```go
// internal/domain/policy/policy_decision.go
package policy

import (
	"errors"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type PolicyDecision struct {
	id                  shared.PolicyDecisionID
	taskID              shared.TaskID
	evaluatedAction     string
	outcome             PolicyOutcome
	constraints         []PolicyConstraint
	approvalRequirement *ApprovalRequirement
	rulesEvaluated      []RuleEvaluation
	reason              string
	createdAt           shared.Timestamp
}

func NewPolicyDecision(
	id shared.PolicyDecisionID,
	taskID shared.TaskID,
	evaluatedAction string,
	outcome PolicyOutcome,
	constraints []PolicyConstraint,
	approvalRequirement *ApprovalRequirement,
	rulesEvaluated []RuleEvaluation,
	reason string,
	now shared.Timestamp,
) (*PolicyDecision, error) {
	if evaluatedAction == "" {
		return nil, errors.New("evaluated action must not be empty")
	}
	if reason == "" {
		return nil, errors.New("policy decision must have a reason")
	}
	if outcome == OutcomeRequireApproval && approvalRequirement == nil {
		return nil, errors.New("require_approval outcome must include approval requirement")
	}
	if outcome == OutcomeAllowWithConstraints && len(constraints) == 0 {
		return nil, errors.New("allow_with_constraints must have at least one constraint")
	}
	return &PolicyDecision{
		id: id, taskID: taskID, evaluatedAction: evaluatedAction,
		outcome: outcome, constraints: constraints,
		approvalRequirement: approvalRequirement, rulesEvaluated: rulesEvaluated,
		reason: reason, createdAt: now,
	}, nil
}

func (d *PolicyDecision) ID() shared.PolicyDecisionID               { return d.id }
func (d *PolicyDecision) TaskID() shared.TaskID                     { return d.taskID }
func (d *PolicyDecision) EvaluatedAction() string                   { return d.evaluatedAction }
func (d *PolicyDecision) Outcome() PolicyOutcome                    { return d.outcome }
func (d *PolicyDecision) Constraints() []PolicyConstraint           { return d.constraints }
func (d *PolicyDecision) ApprovalRequirement() *ApprovalRequirement { return d.approvalRequirement }
func (d *PolicyDecision) RulesEvaluated() []RuleEvaluation          { return d.rulesEvaluated }
func (d *PolicyDecision) Reason() string                            { return d.reason }
func (d *PolicyDecision) CreatedAt() shared.Timestamp               { return d.createdAt }

func ReconstructPolicyDecision(
	id shared.PolicyDecisionID, taskID shared.TaskID, evaluatedAction string,
	outcome PolicyOutcome, constraints []PolicyConstraint,
	approvalRequirement *ApprovalRequirement, rulesEvaluated []RuleEvaluation,
	reason string, createdAt shared.Timestamp,
) *PolicyDecision {
	return &PolicyDecision{
		id: id, taskID: taskID, evaluatedAction: evaluatedAction,
		outcome: outcome, constraints: constraints,
		approvalRequirement: approvalRequirement, rulesEvaluated: rulesEvaluated,
		reason: reason, createdAt: createdAt,
	}
}
```

```go
// internal/domain/policy/evaluator.go
package policy

type EvaluatorResult struct {
	Outcome             PolicyOutcome
	Constraints         []PolicyConstraint
	ApprovalRequirement *ApprovalRequirement
	RulesEvaluated      []RuleEvaluation
	Reason              string
}

// EvaluatePolicy runs all rules sequentially. Most restrictive outcome wins.
func EvaluatePolicy(rules []PolicyRule, ctx PolicyContext) EvaluatorResult {
	var (
		evals               []RuleEvaluation
		mostRestrictive     = OutcomeAllow
		constraints         []PolicyConstraint
		approvalRequirement *ApprovalRequirement
		reason              string
	)

	for _, rule := range rules {
		eval := rule.Evaluate(ctx)
		evals = append(evals, eval)

		if eval.Outcome != nil && eval.Outcome.Restrictiveness() > mostRestrictive.Restrictiveness() {
			mostRestrictive = *eval.Outcome
			reason = eval.Reason
		}
	}

	// Apply defaults if no rule produced an explicit outcome
	if mostRestrictive == OutcomeAllow && reason == "" {
		mostRestrictive = OutcomeAllowWithConstraints
		constraints = []PolicyConstraint{{Type: "timeout", Value: "300s", Reason: "default timeout"}}
		reason = "default: allow with timeout constraint"
	}

	if mostRestrictive == OutcomeRequireApproval && approvalRequirement == nil {
		approvalRequirement = &ApprovalRequirement{
			ApproverRole: "human",
			Reason:       reason,
		}
	}

	return EvaluatorResult{
		Outcome:             mostRestrictive,
		Constraints:         constraints,
		ApprovalRequirement: approvalRequirement,
		RulesEvaluated:      evals,
		Reason:              reason,
	}
}
```

- [ ] **Step 3: Write and run policy evaluator tests**

```go
// internal/domain/policy/evaluator_test.go
package policy_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestTask(t *testing.T, tt task.TaskType, scope task.TaskScope, risk shared.RiskLevel) *task.Task {
	t.Helper()
	tk, err := task.NewTask(
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGCP"), tt, "test", scope,
		shared.PriorityNormal, risk, nil, nil, shared.MustTimestamp(time.Now()),
	)
	require.NoError(t, err)
	return tk
}

func TestPolicyEvaluator_CriticalRisk_RequiresApproval(t *testing.T) {
	tk := makeTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskCritical)
	ctx := policy.PolicyContext{Task: tk, Action: "file_write"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
}

func TestPolicyEvaluator_DestructiveAction_Deny(t *testing.T) {
	tk := makeTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskCritical)
	ctx := policy.PolicyContext{Task: tk, Action: "data_deletion"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeDeny, result.Outcome)
}

func TestPolicyEvaluator_DenyBeatsRequireApproval(t *testing.T) {
	// Critical risk + destructive → deny wins over require_approval
	tk := makeTestTask(t, task.TypeDevelopment, task.ScopeSystem, shared.RiskCritical)
	ctx := policy.PolicyContext{Task: tk, Action: "data_deletion"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeDeny, result.Outcome)
}

func TestPolicyEvaluator_FileScopeLowRisk_Allow(t *testing.T) {
	tk := makeTestTask(t, task.TypeBugfix, task.ScopeFile, shared.RiskLow)
	ctx := policy.PolicyContext{Task: tk, Action: "file_write"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeAllow, result.Outcome)
}

func TestPolicyEvaluator_Default_AllowWithConstraints(t *testing.T) {
	tk := makeTestTask(t, task.TypeResearch, task.ScopeModule, shared.RiskMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "file_read"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeAllowWithConstraints, result.Outcome)
	assert.NotEmpty(t, result.Constraints)
}

func TestPolicyEvaluator_Deployment_RequiresApproval(t *testing.T) {
	tk := makeTestTask(t, task.TypeDeployment, task.ScopeRepo, shared.RiskMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "deployment"}
	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)
	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
}

func TestSensitiveActionClassification(t *testing.T) {
	assert.True(t, policy.IsActionSensitive("shell_execution"))
	assert.True(t, policy.IsActionSensitive("data_deletion"))
	assert.False(t, policy.IsActionSensitive("file_read"))
}
```

Run: `go test ./internal/domain/policy/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/domain/policy/
git commit -m "feat: add policy domain — PolicyDecision, rule evaluation, sensitivity classification"
```

---

## Task 6: Approval + Escalation + Audit Domain (G2: B7+B8+B9)

**Files:**
- Create: `internal/domain/approval/approval_request.go`
- Create: `internal/domain/approval/approval_request_test.go`
- Create: `internal/domain/escalation/escalation_trigger.go`
- Create: `internal/domain/escalation/escalation_trigger_test.go`
- Create: `internal/domain/audit/audit_entry.go`
- Create: `internal/domain/audit/audit_context.go`
- Create: `internal/domain/audit/audit_context_test.go`

These are simpler aggregates. Key patterns: ApprovalRequest has single-resolution invariant. AuditEntry is append-only. AuditContext has builder helpers.

- [ ] **Step 1: Implement ApprovalRequest aggregate**

```go
// internal/domain/approval/approval_request.go
package approval

import (
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type ApprovalStatus string

const (
	StatusPending  ApprovalStatus = "pending"
	StatusApproved ApprovalStatus = "approved"
	StatusDenied   ApprovalStatus = "denied"
	StatusExpired  ApprovalStatus = "expired"
)

func NewApprovalStatus(s string) (ApprovalStatus, error) {
	switch ApprovalStatus(s) {
	case StatusPending, StatusApproved, StatusDenied, StatusExpired:
		return ApprovalStatus(s), nil
	default:
		return "", fmt.Errorf("invalid approval status: %q", s)
	}
}

func (s ApprovalStatus) IsTerminal() bool {
	return s == StatusApproved || s == StatusDenied || s == StatusExpired
}

type ApproverSpec struct {
	Role    string
	ActorID *shared.ActorID
}

type ApprovalResolution struct {
	ResolvedBy shared.ActorID
	ResolvedAt shared.Timestamp
	Reason     string
	Status     ApprovalStatus
}

type ApprovalRequest struct {
	id               shared.ApprovalRequestID
	taskID           shared.TaskID
	workflowRunID    shared.WorkflowRunID
	reason           string
	requiredApprover ApproverSpec
	status           ApprovalStatus
	resolution       *ApprovalResolution
	expiresAt        *shared.Timestamp
	createdAt        shared.Timestamp
}

func NewApprovalRequest(
	id shared.ApprovalRequestID,
	taskID shared.TaskID,
	workflowRunID shared.WorkflowRunID,
	reason string,
	requiredApprover ApproverSpec,
	expiresAt *shared.Timestamp,
	now shared.Timestamp,
) (*ApprovalRequest, error) {
	if reason == "" {
		return nil, fmt.Errorf("approval request must have a reason")
	}
	return &ApprovalRequest{
		id: id, taskID: taskID, workflowRunID: workflowRunID,
		reason: reason, requiredApprover: requiredApprover,
		status: StatusPending, expiresAt: expiresAt, createdAt: now,
	}, nil
}

func (a *ApprovalRequest) ID() shared.ApprovalRequestID   { return a.id }
func (a *ApprovalRequest) TaskID() shared.TaskID          { return a.taskID }
func (a *ApprovalRequest) WorkflowRunID() shared.WorkflowRunID { return a.workflowRunID }
func (a *ApprovalRequest) Reason() string                  { return a.reason }
func (a *ApprovalRequest) RequiredApprover() ApproverSpec   { return a.requiredApprover }
func (a *ApprovalRequest) Status() ApprovalStatus          { return a.status }
func (a *ApprovalRequest) Resolution() *ApprovalResolution { return a.resolution }
func (a *ApprovalRequest) ExpiresAt() *shared.Timestamp    { return a.expiresAt }
func (a *ApprovalRequest) CreatedAt() shared.Timestamp     { return a.createdAt }

func (a *ApprovalRequest) Approve(resolvedBy shared.ActorID, reason string, now shared.Timestamp) error {
	return a.resolve(StatusApproved, resolvedBy, reason, now)
}

func (a *ApprovalRequest) Deny(resolvedBy shared.ActorID, reason string, now shared.Timestamp) error {
	return a.resolve(StatusDenied, resolvedBy, reason, now)
}

func (a *ApprovalRequest) Expire(now shared.Timestamp) error {
	return a.resolve(StatusExpired, shared.ActorID("system"), "approval timed out", now)
}

func (a *ApprovalRequest) resolve(status ApprovalStatus, resolvedBy shared.ActorID, reason string, now shared.Timestamp) error {
	if a.status != StatusPending {
		return fmt.Errorf("cannot resolve approval in status %q, expected pending", a.status)
	}
	a.status = status
	a.resolution = &ApprovalResolution{
		ResolvedBy: resolvedBy,
		ResolvedAt: now,
		Reason:     reason,
		Status:     status,
	}
	return nil
}

func ReconstructApprovalRequest(
	id shared.ApprovalRequestID, taskID shared.TaskID, workflowRunID shared.WorkflowRunID,
	reason string, requiredApprover ApproverSpec, status ApprovalStatus,
	resolution *ApprovalResolution, expiresAt *shared.Timestamp, createdAt shared.Timestamp,
) *ApprovalRequest {
	return &ApprovalRequest{
		id: id, taskID: taskID, workflowRunID: workflowRunID,
		reason: reason, requiredApprover: requiredApprover, status: status,
		resolution: resolution, expiresAt: expiresAt, createdAt: createdAt,
	}
}
```

- [ ] **Step 2: Write and run ApprovalRequest tests**

```go
// internal/domain/approval/approval_request_test.go
package approval_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPendingRequest(t *testing.T) *approval.ApprovalRequest {
	t.Helper()
	now := shared.MustTimestamp(time.Now())
	req, err := approval.NewApprovalRequest(
		shared.ApprovalRequestID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		shared.WorkflowRunID("01HZXK5V6R3QW0F8YJ9N2TMGC3"),
		"critical risk requires human approval",
		approval.ApproverSpec{Role: "human"},
		nil,
		now,
	)
	require.NoError(t, err)
	return req
}

func TestApprovalRequest_Approve(t *testing.T) {
	req := newPendingRequest(t)
	err := req.Approve(shared.ActorID("admin"), "looks good", shared.MustTimestamp(time.Now()))
	require.NoError(t, err)
	assert.Equal(t, approval.StatusApproved, req.Status())
	require.NotNil(t, req.Resolution())
	assert.Equal(t, shared.ActorID("admin"), req.Resolution().ResolvedBy)
}

func TestApprovalRequest_Deny(t *testing.T) {
	req := newPendingRequest(t)
	err := req.Deny(shared.ActorID("admin"), "too risky", shared.MustTimestamp(time.Now()))
	require.NoError(t, err)
	assert.Equal(t, approval.StatusDenied, req.Status())
}

func TestApprovalRequest_DoubleResolution_Fails(t *testing.T) {
	req := newPendingRequest(t)
	require.NoError(t, req.Approve(shared.ActorID("admin"), "ok", shared.MustTimestamp(time.Now())))
	err := req.Deny(shared.ActorID("other"), "changed mind", shared.MustTimestamp(time.Now()))
	assert.Error(t, err)
}

func TestApprovalRequest_MissingReason_Fails(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	_, err := approval.NewApprovalRequest(
		shared.ApprovalRequestID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		shared.WorkflowRunID("01HZXK5V6R3QW0F8YJ9N2TMGC3"),
		"", // empty reason
		approval.ApproverSpec{Role: "human"},
		nil,
		now,
	)
	assert.Error(t, err)
}
```

Run: `go test ./internal/domain/approval/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 3: Implement EscalationTrigger aggregate**

```go
// internal/domain/escalation/escalation_trigger.go
package escalation

import (
	"fmt"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type EscalationTarget string

const (
	TargetHuman      EscalationTarget = "human"
	TargetSeniorAgent EscalationTarget = "senior_agent"
	TargetAdmin      EscalationTarget = "admin"
)

func NewEscalationTarget(s string) (EscalationTarget, error) {
	switch EscalationTarget(s) {
	case TargetHuman, TargetSeniorAgent, TargetAdmin:
		return EscalationTarget(s), nil
	default:
		return "", fmt.Errorf("invalid escalation target: %q", s)
	}
}

type EscalationStatus string

const (
	EscStatusPending   EscalationStatus = "pending"
	EscStatusTriggered EscalationStatus = "triggered"
	EscStatusResolved  EscalationStatus = "resolved"
)

type EscalationCondition struct {
	Type       string // e.g. "timeout_exceeded", "retries_exhausted", "risk_critical"
	Parameters map[string]any
}

type EscalationTrigger struct {
	id          shared.EscalationTriggerID
	taskID      shared.TaskID
	condition   EscalationCondition
	target      EscalationTarget
	status      EscalationStatus
	triggeredAt *shared.Timestamp
	createdAt   shared.Timestamp
}

func NewEscalationTrigger(
	id shared.EscalationTriggerID,
	taskID shared.TaskID,
	condition EscalationCondition,
	target EscalationTarget,
	now shared.Timestamp,
) *EscalationTrigger {
	return &EscalationTrigger{
		id: id, taskID: taskID, condition: condition,
		target: target, status: EscStatusPending, createdAt: now,
	}
}

func (e *EscalationTrigger) ID() shared.EscalationTriggerID { return e.id }
func (e *EscalationTrigger) TaskID() shared.TaskID          { return e.taskID }
func (e *EscalationTrigger) Condition() EscalationCondition  { return e.condition }
func (e *EscalationTrigger) Target() EscalationTarget        { return e.target }
func (e *EscalationTrigger) Status() EscalationStatus        { return e.status }
func (e *EscalationTrigger) TriggeredAt() *shared.Timestamp  { return e.triggeredAt }
func (e *EscalationTrigger) CreatedAt() shared.Timestamp     { return e.createdAt }

func (e *EscalationTrigger) Trigger(now shared.Timestamp) error {
	if e.status != EscStatusPending {
		return fmt.Errorf("cannot trigger escalation in status %q", e.status)
	}
	e.status = EscStatusTriggered
	e.triggeredAt = &now
	return nil
}

func (e *EscalationTrigger) Resolve() error {
	if e.status != EscStatusTriggered {
		return fmt.Errorf("cannot resolve escalation in status %q", e.status)
	}
	e.status = EscStatusResolved
	return nil
}

func ReconstructEscalationTrigger(
	id shared.EscalationTriggerID, taskID shared.TaskID, condition EscalationCondition,
	target EscalationTarget, status EscalationStatus, triggeredAt *shared.Timestamp,
	createdAt shared.Timestamp,
) *EscalationTrigger {
	return &EscalationTrigger{
		id: id, taskID: taskID, condition: condition, target: target,
		status: status, triggeredAt: triggeredAt, createdAt: createdAt,
	}
}
```

- [ ] **Step 4: Write and run EscalationTrigger test**

```go
// internal/domain/escalation/escalation_trigger_test.go
package escalation_test

import (
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscalationTrigger_Trigger(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	et := escalation.NewEscalationTrigger(
		shared.EscalationTriggerID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		escalation.EscalationCondition{Type: "retries_exhausted"},
		escalation.TargetHuman,
		now,
	)
	require.NoError(t, et.Trigger(now))
	assert.Equal(t, escalation.EscStatusTriggered, et.Status())
	assert.NotNil(t, et.TriggeredAt())
}

func TestEscalationTrigger_DoubleTrigger_Fails(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	et := escalation.NewEscalationTrigger(
		shared.EscalationTriggerID("01HZXK5V6R3QW0F8YJ9N2TMGCP"),
		shared.TaskID("01HZXK5V6R3QW0F8YJ9N2TMGC2"),
		escalation.EscalationCondition{Type: "timeout_exceeded"},
		escalation.TargetAdmin,
		now,
	)
	require.NoError(t, et.Trigger(now))
	err := et.Trigger(now)
	assert.Error(t, err)
}
```

Run: `go test ./internal/domain/escalation/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Implement AuditEntry and AuditContext builder**

```go
// internal/domain/audit/audit_entry.go
package audit

import (
	"errors"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type AuditEntry struct {
	id            shared.AuditEntryID
	taskID        *shared.TaskID
	workflowRunID *shared.WorkflowRunID
	actor         shared.ActorID
	action        string
	outcome       string
	context       AuditContext
	createdAt     shared.Timestamp
}

func NewAuditEntry(
	id shared.AuditEntryID,
	actor shared.ActorID,
	action string,
	outcome string,
	context AuditContext,
	now shared.Timestamp,
) (*AuditEntry, error) {
	if action == "" {
		return nil, errors.New("audit entry must have an action")
	}
	if outcome == "" {
		return nil, errors.New("audit entry must have an outcome")
	}
	if context == nil {
		context = NewAuditContext()
	}
	return &AuditEntry{
		id: id, actor: actor, action: action,
		outcome: outcome, context: context, createdAt: now,
	}, nil
}

func (e *AuditEntry) WithTaskID(id shared.TaskID) *AuditEntry           { e.taskID = &id; return e }
func (e *AuditEntry) WithWorkflowRunID(id shared.WorkflowRunID) *AuditEntry { e.workflowRunID = &id; return e }

func (e *AuditEntry) ID() shared.AuditEntryID        { return e.id }
func (e *AuditEntry) TaskID() *shared.TaskID          { return e.taskID }
func (e *AuditEntry) WorkflowRunID() *shared.WorkflowRunID { return e.workflowRunID }
func (e *AuditEntry) Actor() shared.ActorID           { return e.actor }
func (e *AuditEntry) Action() string                  { return e.action }
func (e *AuditEntry) Outcome() string                 { return e.outcome }
func (e *AuditEntry) Context() AuditContext           { return e.context }
func (e *AuditEntry) CreatedAt() shared.Timestamp     { return e.createdAt }

func ReconstructAuditEntry(
	id shared.AuditEntryID, taskID *shared.TaskID, workflowRunID *shared.WorkflowRunID,
	actor shared.ActorID, action string, outcome string, context AuditContext,
	createdAt shared.Timestamp,
) *AuditEntry {
	return &AuditEntry{
		id: id, taskID: taskID, workflowRunID: workflowRunID,
		actor: actor, action: action, outcome: outcome,
		context: context, createdAt: createdAt,
	}
}
```

```go
// internal/domain/audit/audit_context.go
package audit

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// AuditContext provides structured metadata for audit entries.
// Builder helpers prevent chaotic map[string]any drift.
type AuditContext map[string]any

func NewAuditContext() AuditContext {
	return AuditContext{}
}

func (c AuditContext) WithTraceInfo(ctx context.Context) AuditContext {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		c["trace_id"] = span.SpanContext().TraceID().String()
		c["span_id"] = span.SpanContext().SpanID().String()
	}
	return c
}

func (c AuditContext) WithFailure(stage, code string, retryable bool) AuditContext {
	c["failure_stage"] = stage
	c["failure_code"] = code
	c["retryable"] = retryable
	return c
}

func (c AuditContext) WithToolName(name string) AuditContext {
	c["tool_name"] = name
	return c
}

func (c AuditContext) WithStrategy(strategy string) AuditContext {
	c["strategy_used"] = strategy
	return c
}

func (c AuditContext) WithAgentRole(role string) AuditContext {
	c["agent_role"] = role
	return c
}

func (c AuditContext) WithLeaseState(attemptsUsed, retryBudget int, timeElapsedMs int64) AuditContext {
	c["attempts_used"] = attemptsUsed
	c["retry_budget"] = retryBudget
	c["time_elapsed_ms"] = timeElapsedMs
	return c
}

func (c AuditContext) Set(key string, value any) AuditContext {
	c[key] = value
	return c
}
```

- [ ] **Step 6: Write and run AuditContext builder tests**

```go
// internal/domain/audit/audit_context_test.go
package audit_test

import (
	"testing"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/stretchr/testify/assert"
)

func TestAuditContext_Builder(t *testing.T) {
	ctx := audit.NewAuditContext().
		WithStrategy("direct").
		WithAgentRole("implementer").
		WithLeaseState(2, 5, 30000)

	assert.Equal(t, "direct", ctx["strategy_used"])
	assert.Equal(t, "implementer", ctx["agent_role"])
	assert.Equal(t, 2, ctx["attempts_used"])
	assert.Equal(t, 5, ctx["retry_budget"])
	assert.Equal(t, int64(30000), ctx["time_elapsed_ms"])
}

func TestAuditContext_WithFailure(t *testing.T) {
	ctx := audit.NewAuditContext().
		WithFailure("runtime", "tool/shell_timeout", true).
		WithToolName("shell")

	assert.Equal(t, "runtime", ctx["failure_stage"])
	assert.Equal(t, "tool/shell_timeout", ctx["failure_code"])
	assert.Equal(t, true, ctx["retryable"])
	assert.Equal(t, "shell", ctx["tool_name"])
}

func TestAuditContext_Set_CustomKey(t *testing.T) {
	ctx := audit.NewAuditContext().Set("custom_key", 42)
	assert.Equal(t, 42, ctx["custom_key"])
}
```

Run: `go test ./internal/domain/audit/... ./internal/domain/approval/... ./internal/domain/escalation/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/domain/approval/ internal/domain/escalation/ internal/domain/audit/
git commit -m "feat: add approval, escalation, and audit domain — with AuditContext builder"
```

**Checkpoint G2**: All domain aggregates compile, invariants enforced, all domain unit tests pass.

---

## Task 7: Outbound + Inbound Ports (G3: B10+B13)

**Files:**
- Create: `internal/ports/outbound/repositories.go`
- Create: `internal/ports/outbound/memory_context_provider.go`
- Create: `internal/ports/outbound/governance_notifier.go`
- Create: `internal/ports/outbound/clock.go`
- Create: `internal/ports/outbound/id_generator.go`
- Create: `internal/ports/outbound/audit_recorder.go`
- Create: `internal/ports/inbound/governance_service.go`
- Create: `internal/ports/inbound/workflow_control.go`
- Create: `internal/ports/inbound/approval_service.go`
- Create: `internal/ports/inbound/query_service.go`

All interfaces — no implementation. This task is mechanical.

- [ ] **Step 1: Implement outbound port interfaces**

```go
// internal/ports/outbound/repositories.go
package outbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

type TaskRepository interface {
	Save(ctx context.Context, t *task.Task) error
	FindByID(ctx context.Context, id shared.TaskID) (*task.Task, error)
	FindByParentID(ctx context.Context, parentID shared.TaskID) ([]*task.Task, error)
	UpdateStatus(ctx context.Context, t *task.Task) error
}

type WorkflowRunRepository interface {
	Save(ctx context.Context, w *workflow.WorkflowRun) error
	FindByID(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	Update(ctx context.Context, w *workflow.WorkflowRun) error
}

type ExecutionLeaseRepository interface {
	Save(ctx context.Context, l *execution.ExecutionLease) error
	FindByWorkflowRunID(ctx context.Context, wfID shared.WorkflowRunID) (*execution.ExecutionLease, error)
	Update(ctx context.Context, l *execution.ExecutionLease) error
}

type RoutingDecisionRepository interface {
	Save(ctx context.Context, d *routing.RoutingDecision) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
}

type PolicyDecisionRepository interface {
	Save(ctx context.Context, d *policy.PolicyDecision) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*policy.PolicyDecision, error)
}

type ApprovalRequestRepository interface {
	Save(ctx context.Context, a *approval.ApprovalRequest) error
	FindByID(ctx context.Context, id shared.ApprovalRequestID) (*approval.ApprovalRequest, error)
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*approval.ApprovalRequest, error)
	FindPending(ctx context.Context) ([]*approval.ApprovalRequest, error)
	Update(ctx context.Context, a *approval.ApprovalRequest) error
}

type EscalationTriggerRepository interface {
	Save(ctx context.Context, e *escalation.EscalationTrigger) error
	FindByTaskID(ctx context.Context, taskID shared.TaskID) ([]*escalation.EscalationTrigger, error)
	Update(ctx context.Context, e *escalation.EscalationTrigger) error
}

type AuditFilter struct {
	TaskID        *shared.TaskID
	WorkflowRunID *shared.WorkflowRunID
	Actor         *shared.ActorID
	Action        *string
	Limit         int
	Offset        int
}

type AuditEntryRepository interface {
	Append(ctx context.Context, e *audit.AuditEntry) error
	Query(ctx context.Context, filter AuditFilter) ([]*audit.AuditEntry, int, error)
}
```

```go
// internal/ports/outbound/memory_context_provider.go
package outbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// MemoryContextProvider is consumed by routing and intake to enrich decisions.
// Degradable: returns empty context without error if memory-engine is unavailable.
type MemoryContextProvider interface {
	GetRelevantContext(ctx context.Context, taskID shared.TaskID, query string) (*routing.MemoryContext, error)
}
```

```go
// internal/ports/outbound/governance_notifier.go
package outbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

type GovernanceNotifier interface {
	OnExecutionReady(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error
	OnApprovalRequired(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error
	OnWorkflowTerminated(ctx context.Context, wf *workflow.WorkflowRun, reason string) error
}
```

```go
// internal/ports/outbound/clock.go
package outbound

import "github.com/russellcxl/agent-governance-core/internal/domain/shared"

type Clock interface {
	Now() shared.Timestamp
}
```

```go
// internal/ports/outbound/id_generator.go
package outbound

import "github.com/russellcxl/agent-governance-core/internal/domain/shared"

type IDGenerator interface {
	NewTaskID() shared.TaskID
	NewWorkflowRunID() shared.WorkflowRunID
	NewRoutingDecisionID() shared.RoutingDecisionID
	NewPolicyDecisionID() shared.PolicyDecisionID
	NewApprovalRequestID() shared.ApprovalRequestID
	NewExecutionLeaseID() shared.ExecutionLeaseID
	NewEscalationTriggerID() shared.EscalationTriggerID
	NewAuditEntryID() shared.AuditEntryID
}
```

```go
// internal/ports/outbound/audit_recorder.go
package outbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// AuditRecorder is the transversal service port injected into all use cases.
// This is NOT a business feature — it is infrastructure for governance traceability.
type AuditRecorder interface {
	Record(ctx context.Context, actor shared.ActorID, action, outcome string, auditCtx audit.AuditContext, taskID *shared.TaskID, wfID *shared.WorkflowRunID) error
}
```

- [ ] **Step 2: Implement inbound port interfaces**

```go
// internal/ports/inbound/governance_service.go
package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

type SubmitTaskInput struct {
	Type     task.TaskType
	Title    string
	Scope    task.TaskScope
	Priority shared.Priority
	Metadata task.TaskMetadata
}

type ProcessTaskResult struct {
	Task            *task.Task
	RoutingDecision *routing.RoutingDecision
	PolicyDecision  *policy.PolicyDecision
	WorkflowRun     *workflow.WorkflowRun
}

type GovernanceService interface {
	SubmitTask(ctx context.Context, input SubmitTaskInput) (*task.Task, error)
	ProcessTask(ctx context.Context, input SubmitTaskInput, action string) (*ProcessTaskResult, error)
	RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}
```

```go
// internal/ports/inbound/workflow_control.go
package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

type WorkflowControl interface {
	KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error)
}
```

```go
// internal/ports/inbound/approval_service.go
package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type ResolveApprovalInput struct {
	ApprovalRequestID shared.ApprovalRequestID
	Approved          bool
	ResolvedBy        shared.ActorID
	Reason            string
}

type ApprovalService interface {
	ResolveApproval(ctx context.Context, input ResolveApprovalInput) (*approval.ApprovalRequest, error)
	GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error)
}
```

```go
// internal/ports/inbound/query_service.go
package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

type QueryService interface {
	GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error)
	GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error)
}
```

- [ ] **Step 3: Verify everything compiles**

Run: `go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/ports/
git commit -m "feat: add inbound and outbound port interfaces"
```

**Checkpoint G3**: All port interfaces defined. Compiles.

---

## Tasks 8-18: Remaining Implementation

The remaining tasks follow the same TDD pattern established above. Each task has the same structure: write failing test → implement → verify → commit. I'll describe them concisely since the patterns are established.

---

### Task 8: Infrastructure — Clock + IDGenerator (G3)

**Files:**
- Create: `internal/infrastructure/clock/real_clock.go`
- Create: `internal/infrastructure/idgen/ulid_generator.go`

- [ ] **Step 1: Implement RealClock**

```go
// internal/infrastructure/clock/real_clock.go
package clock

import (
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type RealClock struct{}

func (c RealClock) Now() shared.Timestamp {
	return shared.MustTimestamp(time.Now())
}
```

- [ ] **Step 2: Implement ULIDGenerator**

```go
// internal/infrastructure/idgen/ulid_generator.go
package idgen

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

type ULIDGenerator struct{}

func (g ULIDGenerator) generate() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

func (g ULIDGenerator) NewTaskID() shared.TaskID                       { return shared.TaskID(g.generate()) }
func (g ULIDGenerator) NewWorkflowRunID() shared.WorkflowRunID         { return shared.WorkflowRunID(g.generate()) }
func (g ULIDGenerator) NewRoutingDecisionID() shared.RoutingDecisionID { return shared.RoutingDecisionID(g.generate()) }
func (g ULIDGenerator) NewPolicyDecisionID() shared.PolicyDecisionID   { return shared.PolicyDecisionID(g.generate()) }
func (g ULIDGenerator) NewApprovalRequestID() shared.ApprovalRequestID { return shared.ApprovalRequestID(g.generate()) }
func (g ULIDGenerator) NewExecutionLeaseID() shared.ExecutionLeaseID   { return shared.ExecutionLeaseID(g.generate()) }
func (g ULIDGenerator) NewEscalationTriggerID() shared.EscalationTriggerID { return shared.EscalationTriggerID(g.generate()) }
func (g ULIDGenerator) NewAuditEntryID() shared.AuditEntryID           { return shared.AuditEntryID(g.generate()) }
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/clock/ internal/infrastructure/idgen/
git commit -m "feat: add Clock and IDGenerator infrastructure implementations"
```

---

### Task 9: Test Fixtures (G3: B24)

**Files:**
- Create: `test/fixtures/task_factory.go`
- Create: `test/fixtures/workflow_factory.go`
- Create: `test/fixtures/routing_factory.go`
- Create: `test/fixtures/policy_factory.go`
- Create: `test/fixtures/approval_factory.go`
- Create: `test/fixtures/execution_factory.go`

Factories using functional options pattern. Used by all test levels. Example for Task — others follow same pattern:

- [ ] **Step 1: Implement test factories**

```go
// test/fixtures/task_factory.go
package fixtures

import (
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/infrastructure/idgen"
)

type TaskOption func(*taskConfig)

type taskConfig struct {
	id        shared.TaskID
	taskType  task.TaskType
	title     string
	scope     task.TaskScope
	priority  shared.Priority
	riskLevel shared.RiskLevel
	parentID  *shared.TaskID
	metadata  task.TaskMetadata
}

func WithTaskType(t task.TaskType) TaskOption     { return func(c *taskConfig) { c.taskType = t } }
func WithTaskTitle(t string) TaskOption            { return func(c *taskConfig) { c.title = t } }
func WithTaskScope(s task.TaskScope) TaskOption    { return func(c *taskConfig) { c.scope = s } }
func WithTaskPriority(p shared.Priority) TaskOption { return func(c *taskConfig) { c.priority = p } }
func WithTaskRiskLevel(r shared.RiskLevel) TaskOption { return func(c *taskConfig) { c.riskLevel = r } }
func WithTaskParent(id shared.TaskID) TaskOption   { return func(c *taskConfig) { c.parentID = &id } }
func WithTaskMetadata(m task.TaskMetadata) TaskOption { return func(c *taskConfig) { c.metadata = m } }
func WithTaskID(id shared.TaskID) TaskOption       { return func(c *taskConfig) { c.id = id } }

func NewTestTask(opts ...TaskOption) *task.Task {
	gen := idgen.ULIDGenerator{}
	cfg := &taskConfig{
		id:        gen.NewTaskID(),
		taskType:  task.TypeDevelopment,
		title:     "Test task",
		scope:     task.ScopeFile,
		priority:  shared.PriorityNormal,
		riskLevel: shared.RiskLow,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	t, err := task.NewTask(cfg.id, cfg.taskType, cfg.title, cfg.scope, cfg.priority, cfg.riskLevel, cfg.parentID, cfg.metadata, shared.MustTimestamp(time.Now()))
	if err != nil {
		panic(err)
	}
	return t
}
```

Implement similar factories for WorkflowRun, RoutingDecision, PolicyDecision, ApprovalRequest, and ExecutionLease following the same functional options pattern. Each factory provides sensible defaults and allows overriding any field.

- [ ] **Step 2: Verify build**

Run: `go build ./...`

- [ ] **Step 3: Commit**

```bash
git add test/fixtures/
git commit -m "feat: add test fixture factories with functional options"
```

---

### Task 10: Database Migrations (G4: B11 partial)

**Files:**
- Create: `migrations/postgres/001_create_tasks.sql` through `008_create_audit_entries.sql`

- [ ] **Step 1: Write all migration files**

Create each migration file with the exact SQL from the spec (section 8). Each file has `-- +goose Up` and `-- +goose Down` sections. Use goose format for compatibility.

Example for `001_create_tasks.sql`:

```sql
-- +goose Up
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

-- +goose Down
DROP TABLE IF EXISTS tasks;
```

Create migrations 002-008 using the exact SQL from the spec for routing_decisions, policy_decisions, workflow_runs, execution_leases, approval_requests, escalation_triggers, and audit_entries. Note: workflow_runs (004) must come after routing_decisions (002) and policy_decisions (003) due to FK references.

- [ ] **Step 2: Commit**

```bash
git add migrations/
git commit -m "feat: add PostgreSQL migrations for all aggregate tables"
```

---

### Task 11: PostgreSQL Repositories (G4: B11)

**Files:**
- Create: `internal/infrastructure/database/postgres.go`
- Create: `internal/adapters/outbound/persistence/pg_task_repo.go`
- Create: remaining 7 repo files

Each repository implements the port interface using pgx v5. Pattern:

```go
// internal/adapters/outbound/persistence/pg_task_repo.go
package persistence

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

type PgTaskRepository struct {
	pool *pgxpool.Pool
}

func NewPgTaskRepository(pool *pgxpool.Pool) *PgTaskRepository {
	return &PgTaskRepository{pool: pool}
}

func (r *PgTaskRepository) Save(ctx context.Context, t *task.Task) error {
	metadata, _ := json.Marshal(t.Metadata())
	var parentID *string
	if t.ParentTaskID() != nil {
		s := t.ParentTaskID().String()
		parentID = &s
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO tasks (id, parent_task_id, type, title, scope, priority, risk_level, status, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		t.ID().String(), parentID, string(t.Type()), t.Title(), string(t.Scope()),
		string(t.Priority()), string(t.RiskLevel()), string(t.Status()),
		metadata, t.CreatedAt().Time, t.UpdatedAt().Time,
	)
	return err
}

func (r *PgTaskRepository) FindByID(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	var (
		rawID, rawType, title, scope, priority, riskLevel, status string
		parentID                                                   *string
		metadata                                                   []byte
		createdAt, updatedAt                                       = new(interface{}), new(interface{})
	)
	// Implementation: SELECT + scan + task.Reconstruct(...)
	_ = rawID // satisfy compiler
	// Full implementation follows same pattern for all repos
	return nil, nil // placeholder — full implementation in actual task execution
}

// ... remaining methods follow same pattern
```

Implement all 8 repositories following this pattern. Each repo:
- Takes `*pgxpool.Pool` in constructor
- Uses parameterized queries (no SQL injection)
- Uses `Reconstruct*` functions to hydrate domain objects
- Handles JSONB serialization for collections

- [ ] **Step 1: Implement database pool setup**
- [ ] **Step 2: Implement PgTaskRepository (full TDD)**
- [ ] **Step 3: Implement remaining 7 repositories**
- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/database/ internal/adapters/outbound/persistence/
git commit -m "feat: add PostgreSQL repository implementations for all aggregates"
```

---

### Task 12: Memory Context Adapter (G4: B12)

**Files:**
- Create: `internal/adapters/outbound/memory/memory_context_adapter.go`

- [ ] **Step 1: Implement degradable stub adapter**

```go
// internal/adapters/outbound/memory/memory_context_adapter.go
package memory

import (
	"context"
	"log/slog"

	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// StubMemoryContextProvider returns empty context. Phase 1 stub.
// Logs clearly when memory-engine is unavailable for traceability.
type StubMemoryContextProvider struct {
	logger *slog.Logger
}

func NewStubMemoryContextProvider(logger *slog.Logger) *StubMemoryContextProvider {
	return &StubMemoryContextProvider{logger: logger}
}

func (p *StubMemoryContextProvider) GetRelevantContext(ctx context.Context, taskID shared.TaskID, query string) (*routing.MemoryContext, error) {
	p.logger.InfoContext(ctx, "memory-engine unavailable, returning empty context",
		"task_id", taskID.String(),
		"query", query,
		"component", "memory_context_provider",
	)
	return &routing.MemoryContext{}, nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/adapters/outbound/memory/
git commit -m "feat: add degradable memory context provider stub with traceability logging"
```

---

### Task 13: Callback Notifier Adapter (G4)

**Files:**
- Create: `internal/adapters/outbound/events/callback_notifier.go`

- [ ] **Step 1: Implement in-process callback notifier**

```go
// internal/adapters/outbound/events/callback_notifier.go
package events

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

type ExecutionReadyFunc func(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error
type ApprovalRequiredFunc func(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error
type WorkflowTerminatedFunc func(ctx context.Context, wf *workflow.WorkflowRun, reason string) error

type CallbackNotifier struct {
	onExecutionReady     ExecutionReadyFunc
	onApprovalRequired   ApprovalRequiredFunc
	onWorkflowTerminated WorkflowTerminatedFunc
}

func NewCallbackNotifier() *CallbackNotifier {
	return &CallbackNotifier{}
}

func (n *CallbackNotifier) OnExecutionReadyFunc(f ExecutionReadyFunc)         { n.onExecutionReady = f }
func (n *CallbackNotifier) OnApprovalRequiredFunc(f ApprovalRequiredFunc)     { n.onApprovalRequired = f }
func (n *CallbackNotifier) OnWorkflowTerminatedFunc(f WorkflowTerminatedFunc) { n.onWorkflowTerminated = f }

func (n *CallbackNotifier) OnExecutionReady(ctx context.Context, wf *workflow.WorkflowRun, lease *execution.ExecutionLease) error {
	if n.onExecutionReady != nil {
		return n.onExecutionReady(ctx, wf, lease)
	}
	return nil
}

func (n *CallbackNotifier) OnApprovalRequired(ctx context.Context, wf *workflow.WorkflowRun, req *approval.ApprovalRequest) error {
	if n.onApprovalRequired != nil {
		return n.onApprovalRequired(ctx, wf, req)
	}
	return nil
}

func (n *CallbackNotifier) OnWorkflowTerminated(ctx context.Context, wf *workflow.WorkflowRun, reason string) error {
	if n.onWorkflowTerminated != nil {
		return n.onWorkflowTerminated(ctx, wf, reason)
	}
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/adapters/outbound/events/
git commit -m "feat: add in-process callback notifier for governance events"
```

---

### Task 14: Application — AuditRecorder Service (G4: B20)

**Files:**
- Create: `internal/application/audit/record_audit.go`
- Create: `internal/application/audit/query_audit.go`

Implement RecordAuditEntry as a transversal application service (not a business feature). It wraps the AuditEntryRepository and AuditContext builder, extracting trace info from context.

- [ ] **Steps: Implement RecordAuditService + QueryAuditService, test, commit**

---

### Task 15: Application — Intake (G4: B14)

**Files:**
- Create: `internal/application/intake/submit_task.go`
- Create: `internal/application/intake/process_task.go`

Implement SubmitTask (risk classification based on type+scope) and ProcessTask (convenience that chains SubmitTask → RouteTask → EvaluatePolicy → StartWorkflow without bypassing any step).

**ProcessTask rule**: It must NOT grow into a pseudo-orchestrator. It calls each UC in sequence and returns all intermediate results. If any step fails, it returns the error immediately.

- [ ] **Steps: TDD implementation for SubmitTask, then ProcessTask. Commit.**

---

### Task 16: Application — Routing + Policy (G4: B15+B16)

**Files:**
- Create: `internal/application/routing/route_task.go`
- Create: `internal/application/policyeval/evaluate_policy.go`

RouteTask loads task, queries MemoryContextProvider (degradable), runs routing.Evaluate(), creates immutable RoutingDecision, persists, audits.

EvaluatePolicy loads task + routing decision, runs policy.EvaluatePolicy(), creates immutable PolicyDecision, persists, audits.

- [ ] **Steps: TDD implementation for each. Commit.**

---

### Task 17: Application — Workflow Service (G4: B17+B18+B19)

**Files:**
- Create: `internal/application/workflowrun/service.go`
- Create: `internal/application/workflowrun/start_workflow.go`
- Create: `internal/application/workflowrun/kill_workflow.go`
- Create: `internal/application/workflowrun/pause_resume.go`
- Create: `internal/application/workflowrun/register_attempt.go`
- Create: `internal/application/workflowrun/get_status.go`
- Create: `internal/application/approvals/resolve_approval.go`
- Create: `internal/application/escalation/trigger_escalation.go`

WorkflowRunService coordinates Workflow + Execution. StartWorkflow checks RoutingDecision + PolicyDecision exist (no-bypass invariant), branches on policy outcome. RegisterAttempt emits enriched AuditContext with failure telemetry.

**Critical no-bypass tests in this task:**
- No StartWorkflow without RoutingDecision
- No StartWorkflow without PolicyDecision
- No RegisterAttempt on terminal workflow
- No continuation after kill

- [ ] **Steps: TDD implementation for all workflow/approval/escalation UCs. Commit per logical group.**

---

### Task 18: SDK Facade + HTTP Adapter (G5: B22+B23)

**Files:**
- Create: `internal/adapters/inbound/sdk/facade.go`
- Create: `internal/adapters/inbound/http/router.go`
- Create: HTTP handler files

GovernanceFacade composes all 4 inbound port implementations into a single struct. HTTP adapter uses chi router and delegates to the same implementations.

- [ ] **Steps: Implement facade, HTTP handlers, routes. Commit.**

---

### Task 19: Integration Test Infrastructure (G6: B27)

**Files:**
- Create: `test/integration/testhelpers/pg.go`
- Create: `test/integration/persistence/task_repo_test.go` (and remaining repos)

Set up testcontainers-go with PostgreSQL, migration runner, and write integration tests for all repositories.

- [ ] **Steps: Setup testcontainers helper, write repo integration tests. Commit.**

---

### Task 20: Use-Case Integration Tests (G6: B28)

**Files:**
- Create: `test/integration/usecases/process_task_test.go`

End-to-end tests with real PostgreSQL:
- Happy path: allow → completed
- Approval flow: require_approval → approved → completed
- Deny flow: deny → failed
- Kill mid-execution
- Budget exhausted
- Decompose creates subtasks

- [ ] **Steps: Write full pipeline integration tests. Commit.**

---

### Task 21: Wiring + Configuration (G7: B29)

**Files:**
- Modify: `cmd/agent-governance-core/main.go`
- Create: `internal/infrastructure/config/config.go`

Wire all dependencies: config → pgx pool → repos → clock → idgen → memory adapter → notifier → use cases → facade → HTTP router → server start.

- [ ] **Steps: Implement DI wiring, config loading, server startup. Verify build + boot. Commit.**

---

## Execution Group Summary

| Group | Tasks | Parallel | Checkpoint |
|---|---|---|---|
| **G1** | Task 1 | No | VOs compile, enums validate |
| **G2** | Tasks 2-6 | Yes (all domain) | All aggregates compile, invariants enforced |
| **G3** | Tasks 7-9 | Yes | Ports defined, infra impls, fixtures ready |
| **G4** | Tasks 10-17 | Partially (migrations first, then repos + UCs) | Full pipeline works via SDK |
| **G5** | Task 18 | No | SDK facade + HTTP operational |
| **G6** | Tasks 19-20 | No (19 first, then 20) | All integration tests pass with real PG |
| **G7** | Task 21 | No | Application boots end-to-end |

## Implementation Matices Checklist

At each checkpoint, verify:

- [ ] `WorkflowStatus` is source of truth — `TaskStatus` never duplicates execution state
- [ ] `RecordAuditEntry` treated as transversal service, not independent business feature
- [ ] `MemoryContextProvider` logs clearly when memory unavailable
- [ ] `AuditContext` uses builder helpers, no raw map manipulation
- [ ] `ProcessTask` stays convenience — chains UCs without growing into orchestrator
