package policyeval_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/policyeval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	domainrouting "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
)

// --- Inline mocks ---

type mockTaskRepo struct {
	tasks map[shared.TaskID]*task.Task
}

func (m *mockTaskRepo) FindByID(_ context.Context, id shared.TaskID) (*task.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	return t, nil
}

func (m *mockTaskRepo) Save(_ context.Context, _ *task.Task) error { return nil }
func (m *mockTaskRepo) FindByParentID(_ context.Context, _ shared.TaskID) ([]*task.Task, error) {
	return nil, nil
}
func (m *mockTaskRepo) UpdateStatus(_ context.Context, _ *task.Task) error { return nil }

type mockRoutingRepo struct {
	mu        sync.Mutex
	decisions map[shared.TaskID]*domainrouting.RoutingDecision
}

func (m *mockRoutingRepo) Save(_ context.Context, rd *domainrouting.RoutingDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decisions == nil {
		m.decisions = make(map[shared.TaskID]*domainrouting.RoutingDecision)
	}
	m.decisions[rd.TaskID()] = rd
	return nil
}

func (m *mockRoutingRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*domainrouting.RoutingDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, ok := m.decisions[taskID]
	if !ok {
		return nil, fmt.Errorf("routing decision not found for task: %s", taskID)
	}
	return rd, nil
}

type mockPolicyRepo struct {
	mu        sync.Mutex
	decisions map[shared.TaskID]*policy.PolicyDecision
}

func (m *mockPolicyRepo) Save(_ context.Context, pd *policy.PolicyDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decisions == nil {
		m.decisions = make(map[shared.TaskID]*policy.PolicyDecision)
	}
	m.decisions[pd.TaskID()] = pd
	return nil
}

func (m *mockPolicyRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*policy.PolicyDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pd, ok := m.decisions[taskID]
	if !ok {
		return nil, fmt.Errorf("policy decision not found for task: %s", taskID)
	}
	return pd, nil
}

type mockWfRepo struct {
	mu  sync.Mutex
	wfs map[shared.TaskID]*workflow.WorkflowRun
}

func (m *mockWfRepo) Save(_ context.Context, wf *workflow.WorkflowRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.wfs == nil {
		m.wfs = make(map[shared.TaskID]*workflow.WorkflowRun)
	}
	m.wfs[wf.TaskID] = wf
	return nil
}

func (m *mockWfRepo) FindByID(_ context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, wf := range m.wfs {
		if wf.ID == id {
			return wf, nil
		}
	}
	return nil, fmt.Errorf("workflow run not found: %s", id)
}

func (m *mockWfRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wf, ok := m.wfs[taskID]
	if !ok {
		return nil, fmt.Errorf("workflow run not found for task: %s", taskID)
	}
	return wf, nil
}

func (m *mockWfRepo) Update(_ context.Context, wf *workflow.WorkflowRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wfs[wf.TaskID] = wf
	return nil
}

func (m *mockWfRepo) List(_ context.Context, _ outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}

type mockIDGen struct {
	mu      sync.Mutex
	counter int
}

func (m *mockIDGen) next() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter++
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

func (m *mockIDGen) NewTaskID() shared.TaskID {
	id, _ := shared.NewTaskID(m.next())
	return id
}

func (m *mockIDGen) NewWorkflowRunID() shared.WorkflowRunID {
	id, _ := shared.NewWorkflowRunID(m.next())
	return id
}

func (m *mockIDGen) NewRoutingDecisionID() shared.RoutingDecisionID {
	id, _ := shared.NewRoutingDecisionID(m.next())
	return id
}

func (m *mockIDGen) NewPolicyDecisionID() shared.PolicyDecisionID {
	id, _ := shared.NewPolicyDecisionID(m.next())
	return id
}

func (m *mockIDGen) NewApprovalRequestID() shared.ApprovalRequestID {
	id, _ := shared.NewApprovalRequestID(m.next())
	return id
}

func (m *mockIDGen) NewExecutionLeaseID() shared.ExecutionLeaseID {
	id, _ := shared.NewExecutionLeaseID(m.next())
	return id
}

func (m *mockIDGen) NewEscalationTriggerID() shared.EscalationTriggerID {
	id, _ := shared.NewEscalationTriggerID(m.next())
	return id
}

func (m *mockIDGen) NewAuditEntryID() shared.AuditEntryID {
	id, _ := shared.NewAuditEntryID(m.next())
	return id
}

type mockClock struct {
	fixedTime shared.Timestamp
}

func newMockClock() *mockClock {
	return &mockClock{
		fixedTime: shared.MustTimestamp(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)),
	}
}

func (m *mockClock) Now() shared.Timestamp { return m.fixedTime }

type auditCall struct {
	actor   shared.ActorID
	action  string
	outcome string
	ctx     audit.AuditContext
	taskID  *shared.TaskID
	wfID    *shared.WorkflowRunID
}

type mockAuditRecorder struct {
	mu    sync.Mutex
	calls []auditCall
}

func (m *mockAuditRecorder) Record(_ context.Context, actor shared.ActorID, action, outcome string, ctx audit.AuditContext, taskID *shared.TaskID, wfID *shared.WorkflowRunID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, auditCall{
		actor:   actor,
		action:  action,
		outcome: outcome,
		ctx:     ctx,
		taskID:  taskID,
		wfID:    wfID,
	})
	return nil
}

// --- Helpers ---

// setupRoutedWorkflow creates a task, a routing decision for it, and a workflow in routed state.
func setupRoutedWorkflow(t *testing.T, opts ...fixtures.TaskOption) (*task.Task, *mockRoutingRepo, *mockWfRepo) {
	t.Helper()
	tk := fixtures.NewTestTask(opts...)
	rd := fixtures.NewTestRoutingDecision(fixtures.WithRoutingTaskID(tk.ID()))

	routingRepo := &mockRoutingRepo{
		decisions: map[shared.TaskID]*domainrouting.RoutingDecision{tk.ID(): rd},
	}

	wf := fixtures.NewTestWorkflowRun(fixtures.WithWorkflowTaskID(tk.ID()))
	now := shared.MustTimestamp(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC))
	actor := shared.ActorID("system")
	err := wf.MarkRouted(rd.ID(), "test routing", actor, now)
	require.NoError(t, err)

	wfRepo := &mockWfRepo{
		wfs: map[shared.TaskID]*workflow.WorkflowRun{tk.ID(): wf},
	}

	return tk, routingRepo, wfRepo
}

// --- Tests ---

func TestEvaluatePolicy_CreatesPolicyDecisionAndPersists(t *testing.T) {
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t)
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "code_edit")

	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Equal(t, tk.ID(), pd.TaskID())
	assert.Equal(t, "code_edit", pd.EvaluatedAction())
	assert.NotEmpty(t, pd.Reason())

	// Verify persisted
	saved, err := policyRepo.FindByTaskID(context.Background(), tk.ID())
	require.NoError(t, err)
	assert.Equal(t, pd.ID(), saved.ID())
}

func TestEvaluatePolicy_TransitionsWorkflowToPolicyChecked(t *testing.T) {
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t)
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	_, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "code_edit")

	require.NoError(t, err)

	wf, err := wfRepo.FindByTaskID(context.Background(), tk.ID())
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusPolicyChecked, wf.Status)
	assert.NotNil(t, wf.PolicyDecisionID)
}

func TestEvaluatePolicy_RecordsAuditEntry(t *testing.T) {
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t)
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	_, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "code_edit")

	require.NoError(t, err)
	require.Len(t, auditRec.calls, 1)
	assert.Equal(t, shared.ActorID("system"), auditRec.calls[0].actor)
	assert.Equal(t, "policy_evaluated", auditRec.calls[0].action)
	assert.NotEmpty(t, auditRec.calls[0].outcome)
	assert.NotNil(t, auditRec.calls[0].taskID)
}

func TestEvaluatePolicy_FailsWhenTaskNotFound(t *testing.T) {
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{}}
	routingRepo := &mockRoutingRepo{}
	policyRepo := &mockPolicyRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	unknownID, _ := shared.NewTaskID(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
	pd, err := svc.EvaluatePolicy(context.Background(), unknownID, "code_edit")

	require.Error(t, err)
	assert.Nil(t, pd)
	assert.Contains(t, err.Error(), "task not found")
}

func TestEvaluatePolicy_FailsWhenRoutingDecisionNotFound(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{} // empty — no routing decision
	policyRepo := &mockPolicyRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "code_edit")

	require.Error(t, err)
	assert.Nil(t, pd)
	assert.Contains(t, err.Error(), "routing decision required")
}

func TestEvaluatePolicy_CriticalRiskRequiresApproval(t *testing.T) {
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t, fixtures.WithTaskRiskLevel(shared.RiskLevelCritical))
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "code_edit")

	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Equal(t, policy.OutcomeRequireApproval, pd.Outcome())
	assert.NotNil(t, pd.ApprovalRequirement())
}

func TestEvaluatePolicy_DestructiveActionDenied(t *testing.T) {
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t)
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "data_deletion")

	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Equal(t, policy.OutcomeDeny, pd.Outcome())
}

// TestEvaluatePolicy_IL2_SddApply_TasksDone_Passes verifies that the full policy
// evaluation service does NOT deny sdd_apply when tasks_phase_status=done. Spec #47.
func TestEvaluatePolicy_IL2_SddApply_TasksDone_Passes(t *testing.T) {
	meta := task.TaskMetadata{"tasks_phase_status": "done"}
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t, fixtures.WithTaskMetadata(meta))
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "sdd_apply")

	require.NoError(t, err)
	require.NotNil(t, pd)
	// With tasks_phase_status=done, IL2 passes. No deny outcome.
	assert.NotEqual(t, policy.OutcomeDeny, pd.Outcome())
}

// TestEvaluatePolicy_IL2_SddApply_TasksRunning_Denied verifies that the full policy
// evaluation service denies sdd_apply when tasks_phase_status=running. Spec #47.
func TestEvaluatePolicy_IL2_SddApply_TasksRunning_Denied(t *testing.T) {
	meta := task.TaskMetadata{"tasks_phase_status": "running"}
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t, fixtures.WithTaskMetadata(meta))
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "sdd_apply")

	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Equal(t, policy.OutcomeDeny, pd.Outcome())
}

// TestEvaluatePolicy_IL2_SddApply_TasksBlocked_Denied verifies that the full policy
// evaluation service denies sdd_apply when tasks_phase_status=blocked. Spec #47.
func TestEvaluatePolicy_IL2_SddApply_TasksBlocked_Denied(t *testing.T) {
	meta := task.TaskMetadata{"tasks_phase_status": "blocked"}
	tk, routingRepo, wfRepo := setupRoutedWorkflow(t, fixtures.WithTaskMetadata(meta))
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	policyRepo := &mockPolicyRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := policyeval.NewEvaluatePolicyService(taskRepo, routingRepo, policyRepo, wfRepo, idGen, clock, auditRec)
	pd, err := svc.EvaluatePolicy(context.Background(), tk.ID(), "sdd_apply")

	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Equal(t, policy.OutcomeDeny, pd.Outcome())
}
