# Phase 3 Track 2.1: DLQ / Quarantine — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `quarantined` terminal state to the workflow state machine — workflows that exhaust their retry budget are quarantined (not generic-failed), queryable, and visible for operational investigation.

**Architecture:** New terminal state in the existing state machine (additive change). Single transition `running → quarantined` triggered by budget exhaustion in RegisterAttempt. New `ListWorkflows` query with status filter for operational visibility. No schema migrations — existing TEXT column absorbs the new status value.

**Tech Stack:** Go 1.26.2, PostgreSQL 16+, pgx v5, testcontainers-go

**Spec:** `docs/superpowers/specs/2026-04-16-phase3-track2-dlq-quarantine-design.md`

**Baseline invariant:** All existing tests must pass. Only budget exhaustion triggers quarantine. Policy deny/approval denied/kill are unaffected.

---

## File Structure

```
internal/
  domain/workflow/
    status.go                              — MODIFY: add StatusQuarantined, update IsTerminal, update transitions
    workflow_run.go                         — MODIFY: add Quarantine() method
    workflow_run_test.go                    �� MODIFY: add quarantine tests
  application/workflowrun/
    register_attempt.go                    — MODIFY: budget exhausted → Quarantine + quarantine audit
  ports/
    outbound/repositories.go               — MODIFY: add WorkflowListFilter, add List to WorkflowRunRepository
    inbound/query_service.go               — MODIFY: add ListWorkflows
  adapters/
    outbound/persistence/
      pg_workflow_run_repo.go              — MODIFY: implement List with COUNT(*) OVER()
    inbound/
      http/
        router.go                          — MODIFY: add GET /api/v1/workflows route
        workflow_handler.go                ��� MODIFY: add handleListWorkflows handler
      sdk/
        facade.go                          — MODIFY: add ListWorkflows delegation
  bootstrap/
    wire.go                                — MODIFY: update queryServiceAdapter
test/integration/
  quarantine/
    quarantine_test.go                     — NEW: e2e quarantine flow + ListWorkflows
```

---

## Task 1: Domain — Quarantined State (B1)

**Files:**
- Modify: `internal/domain/workflow/status.go`
- Modify: `internal/domain/workflow/workflow_run.go`
- Modify: `internal/domain/workflow/workflow_run_test.go`

- [ ] **Step 1: Write failing tests for quarantine**

Add to `internal/domain/workflow/workflow_run_test.go`:

```go
func TestWorkflowRun_Quarantine_FromRunning(t *testing.T) {
	wf := runningWorkflow(t)
	err := wf.Quarantine("retry budget exhausted", testActor, testNow)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusQuarantined, wf.Status())
}

func TestWorkflowRun_Quarantined_IsTerminal(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Quarantine("budget exhausted", testActor, testNow))
	assert.True(t, wf.Status().IsTerminal())
}

func TestWorkflowRun_NoTransitionFromQuarantined(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Quarantine("budget exhausted", testActor, testNow))
	err := wf.TransitionTo(workflow.StatusRunning, "retry", testActor, testNow)
	assert.Error(t, err)
}

func TestWorkflowRun_KillFromQuarantined_Fails(t *testing.T) {
	wf := runningWorkflow(t)
	require.NoError(t, wf.Quarantine("budget exhausted", testActor, testNow))
	err := wf.Kill("too late", testActor, testNow)
	assert.Error(t, err)
}

func TestNewWorkflowStatus_Quarantined_Valid(t *testing.T) {
	s, err := workflow.NewWorkflowStatus("quarantined")
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusQuarantined, s)
}
```

Note: `runningWorkflow(t)` is an existing helper in the test file — READ it to confirm it exists and what it returns.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/workflow/... -v -run "TestWorkflowRun_Quarantine" -count=1`
Expected: FAIL — `StatusQuarantined` and `Quarantine()` not defined

- [ ] **Step 3: Add StatusQuarantined to status.go**

Modify `internal/domain/workflow/status.go`:

Add the constant:
```go
const (
	// ... existing constants ...
	StatusQuarantined WorkflowStatus = "quarantined"
)
```

Update `terminalStates`:
```go
var terminalStates = map[WorkflowStatus]bool{
	StatusCompleted:   true,
	StatusFailed:      true,
	StatusKilled:      true,
	StatusQuarantined: true,
}
```

Update `transitionTable` — add `StatusQuarantined` to `StatusRunning` targets:
```go
StatusRunning: {
	StatusCompleted:   true,
	StatusFailed:      true,
	StatusPaused:      true,
	StatusKilled:      true,
	StatusQuarantined: true,
},
```

Update `NewWorkflowStatus` (or whatever validates the enum) to accept `"quarantined"`. READ the actual validator — it may use a switch or a map. Add the new value.

- [ ] **Step 4: Add Quarantine() method to workflow_run.go**

Add to `internal/domain/workflow/workflow_run.go`:

```go
// Quarantine transitions from running → quarantined (budget exhausted).
func (w *WorkflowRun) Quarantine(reason string, actor shared.ActorID, now shared.Timestamp) error {
	return w.transitionTo(StatusQuarantined, reason, actor, now)
}
```

Same pattern as `Fail()`, `Complete()`, etc.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/domain/workflow/... -v -count=1`
Expected: ALL PASS (existing + new quarantine tests)

- [ ] **Step 6: Verify full build + no regressions**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/domain/workflow/
git commit -m "feat(dlq): add quarantined terminal state to workflow state machine"
```

---

## Task 2: Application + Ports (B2)

**Files:**
- Modify: `internal/application/workflowrun/register_attempt.go`
- Modify: `internal/ports/outbound/repositories.go`
- Modify: `internal/ports/inbound/query_service.go`

- [ ] **Step 1: Change RegisterAttempt budget exhausted to quarantine**

Modify `internal/application/workflowrun/register_attempt.go`:

Change line 58 (the budget exhausted branch):

From:
```go
if err := wf.Fail("retry budget exhausted", actor, now); err != nil {
	return nil, fmt.Errorf("failing workflow: %w", err)
}
```

To:
```go
if err := wf.Quarantine("retry budget exhausted", actor, now); err != nil {
	return nil, fmt.Errorf("quarantining workflow: %w", err)
}
```

- [ ] **Step 2: Add quarantine-specific audit entry**

In the same file, after the existing audit call (line 94) and before the notify block (line 97), add the quarantine-specific audit entry:

```go
// 8. Record audit
wfID := wf.ID
_ = s.audit.Record(ctx, actor, "attempt_registered", string(wf.Status), auditCtx, nil, &wfID)

// 8b. Emit dedicated quarantine audit entry (once per effective transition)
if wf.Status == workflow.StatusQuarantined {
	_ = s.audit.Record(ctx, actor, "workflow_quarantined", "budget_exhausted", auditCtx, nil, &wfID)
}
```

- [ ] **Step 3: Update notifier reason for quarantine**

Change the notifier call (line 98). Currently:
```go
_ = s.notifier.OnWorkflowTerminated(ctx, wf, string(wf.Status))
```

Change to use normalized reason format for quarantine:
```go
if wf.Status.IsTerminal() {
	reason := string(wf.Status)
	if wf.Status == workflow.StatusQuarantined {
		reason = "quarantined:budget_exhausted"
	}
	_ = s.notifier.OnWorkflowTerminated(ctx, wf, reason)
}
```

- [ ] **Step 4: Add WorkflowListFilter and List to outbound ports**

Modify `internal/ports/outbound/repositories.go`:

Add the filter struct (after `AuditFilter`):
```go
// WorkflowListFilter defines filter criteria for listing workflow runs.
type WorkflowListFilter struct {
	Status *workflow.WorkflowStatus
	TaskID *shared.TaskID
	Limit  int
	Offset int
}
```

Add `List` method to `WorkflowRunRepository`:
```go
type WorkflowRunRepository interface {
	Save(ctx context.Context, wf *workflow.WorkflowRun) error
	FindByID(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	FindByTaskID(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	Update(ctx context.Context, wf *workflow.WorkflowRun) error
	List(ctx context.Context, filter WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) // NEW
}
```

- [ ] **Step 5: Add ListWorkflows to QueryService**

Modify `internal/ports/inbound/query_service.go`:

```go
type QueryService interface {
	GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error)
	GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error)
	ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) // NEW
}
```

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: FAIL — `List` not implemented on PgWorkflowRunRepository, `ListWorkflows` not implemented on queryServiceAdapter, facade, etc. That's expected — those are in Task 3. But the domain and application code should compile.

Actually, since the port interface changed, ALL implementors must implement `List` for the build to pass. We need to add stub implementations before build will pass. Add a stub to the PG repo:

If build fails, temporarily add a stub to `pg_workflow_run_repo.go`:
```go
func (r *PgWorkflowRunRepository) List(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, fmt.Errorf("not implemented")
}
```

And to the query adapter in `wire.go`:
```go
func (q *queryServiceAdapter) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, fmt.Errorf("not implemented")
}
```

And to the facade and mock repos in test files — search for all implementors:
```bash
rg "WorkflowRunRepository" --files-with-matches
rg "QueryService" --files-with-matches
```

Update ALL to include the new method (stub or real). This is necessary because Go interfaces require complete implementation.

- [ ] **Step 7: Update existing RegisterAttempt tests**

READ `internal/application/workflowrun/service_test.go` — find the test for budget exhaustion. It currently asserts `workflow.StatusFailed`. Change to `workflow.StatusQuarantined`.

Also add a test specifically for the quarantine audit entry and notifier reason:

```go
func TestRegisterAttempt_BudgetExhausted_Quarantined(t *testing.T) {
	// ... setup with maxRetries=1 ...
	// Register failure attempt that exhausts budget
	// Assert: wf.Status == workflow.StatusQuarantined (not Failed)
	// Assert: audit.Record called with "workflow_quarantined" action
	// Assert: notifier called with "quarantined:budget_exhausted" reason
}
```

- [ ] **Step 8: Run build + all tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 9: Commit**

```bash
git add internal/application/workflowrun/ internal/ports/
git commit -m "feat(dlq): RegisterAttempt quarantines on budget exhaustion, add ListWorkflows ports"
```

---

## Task 3: Persistence + HTTP + Wiring (B3)

**Files:**
- Modify: `internal/adapters/outbound/persistence/pg_workflow_run_repo.go`
- Modify: `internal/adapters/inbound/http/router.go`
- Modify: `internal/adapters/inbound/http/workflow_handler.go`
- Modify: `internal/adapters/inbound/sdk/facade.go`
- Modify: `internal/bootstrap/wire.go`

- [ ] **Step 1: Implement List on PgWorkflowRunRepository**

Replace the stub in `pg_workflow_run_repo.go` with real implementation:

```go
func (r *PgWorkflowRunRepository) List(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	var conditions []string
	var args []any
	argIdx := 1

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}
	if filter.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argIdx))
		args = append(args, filter.TaskID.String())
		argIdx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	var limitClause string
	if filter.Limit > 0 {
		limitClause = fmt.Sprintf("LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
		args = append(args, filter.Limit, filter.Offset)
	}

	query := fmt.Sprintf(`
		SELECT id, task_id, status, routing_decision_id, policy_decision_id,
		       current_step_index, transitions, created_at, updated_at,
		       COUNT(*) OVER() AS total_count
		FROM workflow_runs %s
		ORDER BY updated_at DESC
		%s`, whereClause, limitClause)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listing workflow runs: %w", err)
	}
	defer rows.Close()

	var results []*workflow.WorkflowRun
	var total int
	for rows.Next() {
		// Scan all columns including total_count
		// READ the existing scanWorkflowRun or FindByID to understand the scanning pattern
		// You need to scan: id, task_id, status, routing_decision_id, policy_decision_id,
		//                    current_step_index, transitions (JSONB), created_at, updated_at, total_count
		// Use ReconstructWorkflowRun for hydration
	}
	// ... error handling, return results, total, nil
}
```

READ `pg_workflow_run_repo.go` to understand the existing scan pattern (how JSONB transitions are deserialized, how nullable IDs are handled). Follow the exact same pattern.

- [ ] **Step 2: Add handleListWorkflows HTTP handler**

Add to `internal/adapters/inbound/http/workflow_handler.go`:

```go
func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	limitParam := r.URL.Query().Get("limit")
	offsetParam := r.URL.Query().Get("offset")

	filter := outbound.WorkflowListFilter{
		Limit:  20, // default
		Offset: 0,
	}

	if statusParam != "" {
		status, err := workflow.NewWorkflowStatus(statusParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS", err.Error())
			return
		}
		filter.Status = &status
	}
	if limitParam != "" {
		l, err := strconv.Atoi(limitParam)
		if err == nil && l > 0 && l <= 100 {
			filter.Limit = l
		}
	}
	if offsetParam != "" {
		o, err := strconv.Atoi(offsetParam)
		if err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	workflows, total, err := s.queries.ListWorkflows(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}

	// Convert to response DTOs
	var items []workflowRunResponse
	for _, wf := range workflows {
		items = append(items, workflowToResponse(wf))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}
```

Add `"strconv"` to imports.

- [ ] **Step 3: Add route for ListWorkflows**

Modify `internal/adapters/inbound/http/router.go` — add before the `{workflowID}` routes:

```go
// Workflows
r.Get("/workflows", s.handleListWorkflows)          // NEW — list with filters
r.Get("/workflows/{workflowID}", s.handleGetWorkflowStatus)
```

**IMPORTANT:** The `GET /workflows` route MUST be registered BEFORE `GET /workflows/{workflowID}` in chi, otherwise `{workflowID}` will match "workflows" as the ID. Actually in chi, explicit routes take precedence over parameterized routes, but verify by testing.

- [ ] **Step 4: Add ListWorkflows to facade**

Modify `internal/adapters/inbound/sdk/facade.go`:

```go
func (f *GovernanceFacade) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return f.queries.ListWorkflows(ctx, filter)
}
```

- [ ] **Step 5: Update queryServiceAdapter in wire.go**

Modify `internal/bootstrap/wire.go` — update `queryServiceAdapter` to implement `ListWorkflows`:

```go
func (q *queryServiceAdapter) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return q.workflowRepo.List(ctx, filter)
}
```

Add `workflowRepo` field to `queryServiceAdapter` if not already there (it may use `workflowSvc` — check). READ the actual struct and add the repo reference needed.

- [ ] **Step 6: Update all mock/test implementors of the changed interfaces**

Search for all files that implement `WorkflowRunRepository` or `QueryService` and add the `List` / `ListWorkflows` methods:

```bash
rg "WorkflowRunRepository" --files-with-matches
rg "QueryService" --files-with-matches
```

Each mock in test files needs:
```go
func (m *mockWfRepo) List(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}
```

And for QueryService mocks:
```go
func (m *mockQueryService) ListWorkflows(ctx context.Context, filter outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}
```

- [ ] **Step 7: Run build + all tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/adapters/ internal/bootstrap/
git commit -m "feat(dlq): implement ListWorkflows endpoint and quarantine query support"
```

---

## Task 4: Integration Test (B4)

**Files:**
- Create: `test/integration/quarantine/quarantine_test.go`

- [ ] **Step 1: Write integration test for quarantine flow + ListWorkflows**

```go
//go:build integration

package quarantine_test
```

**Test 1: Full pipeline → budget exhausted → quarantined in DB**

Wire full application stack with real PG (follow pattern from `test/integration/usecases/process_task_test.go`). Execute:
1. Submit task (bugfix, file, low risk)
2. Route task
3. Evaluate policy (file_write → allow)
4. Start workflow → running
5. Register 3 failure attempts (maxRetries=3 by default) → budget exhausted → quarantined

Assert:
- Workflow status is `quarantined` (not `failed`)
- Workflow persisted correctly — reload from DB and verify
- Audit entries include both `attempt_registered` with outcome `quarantined` AND `workflow_quarantined` with outcome `budget_exhausted`

**Test 2: ListWorkflows with status=quarantined**

After test 1 creates a quarantined workflow, also create a normal completed workflow. Then:
1. Call `List(ctx, WorkflowListFilter{Status: &quarantinedStatus, Limit: 10})` on the repo
2. Assert only the quarantined workflow is returned
3. Assert total = 1

**Test 3: Policy deny still goes to failed (NOT quarantined)**

Submit a task, route, evaluate policy with `data_deletion` action (→ deny), start workflow (→ failed).
Assert: status is `failed`, NOT `quarantined`.

READ these files to understand wiring:
- `test/integration/usecases/process_task_test.go` — full wiring pattern
- `test/integration/adaptive/adaptive_routing_test.go` — similar pattern

**IMPORTANT:** The `NewWorkflowRunService` constructor has a `lifecycleMetrics` param — pass `nil`. The `NewRouteTaskService` has a `statsStore` param — pass `nil`. Check all constructors.

- [ ] **Step 2: Run integration test**

Run: `go test ./test/integration/quarantine/... -v -count=1 -tags=integration -timeout=600s`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/quarantine/
git commit -m "feat(dlq): add integration tests for quarantine flow and ListWorkflows"
```

---

## Verification Checklist

After all tasks:

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./internal/... ./test/fixtures/... -count=1` — ALL PASS (zero regressions)
- [ ] `go test ./test/integration/... -v -count=1 -tags=integration -timeout=600s` ��� ALL PASS
- [ ] Budget exhausted → `quarantined` (not `failed`)
- [ ] Policy deny → `failed` (NOT quarantined)
- [ ] Approval denied → `failed` (NOT quarantined)
- [ ] Kill → `killed` (NOT quarantined)
- [ ] `quarantined` is terminal — no transitions out
- [ ] Two audit entries on quarantine: `attempt_registered` + `workflow_quarantined`
- [ ] Notifier called with `"quarantined:budget_exhausted"` reason
- [ ] `GET /api/v1/workflows?status=quarantined` returns quarantined workflows
- [ ] ListWorkflows pagination works (total count correct)
- [ ] No database migrations needed
