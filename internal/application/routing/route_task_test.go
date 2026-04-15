package routing_test

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

	approuting "github.com/russellcxl/agent-governance-core/internal/application/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	domainrouting "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/test/fixtures"
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

func (m *mockTaskRepo) Save(_ context.Context, _ *task.Task) error            { return nil }
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

type mockMemoryProvider struct {
	memCtx *domainrouting.MemoryContext
	err    error
}

func (m *mockMemoryProvider) GetRelevantContext(_ context.Context, _ shared.TaskID, _ string) (*domainrouting.MemoryContext, error) {
	return m.memCtx, m.err
}

// --- Tests ---

func TestRouteTask_LoadsTaskCreatesDecisionAndPersists(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}
	memProvider := &mockMemoryProvider{memCtx: &domainrouting.MemoryContext{}}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, memProvider)
	rd, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.NotNil(t, rd)
	assert.Equal(t, tk.ID(), rd.TaskID())
	assert.NotEmpty(t, rd.SelectedStrategy())
	assert.NotEmpty(t, rd.Reason())

	// Verify persisted
	saved, err := routingRepo.FindByTaskID(context.Background(), tk.ID())
	require.NoError(t, err)
	assert.Equal(t, rd.ID(), saved.ID())
}

func TestRouteTask_CreatesWorkflowRunIfNoneExists(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{} // empty — no workflow exists
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	rd, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.NotNil(t, rd)

	// Verify WorkflowRun was created and transitioned to routed
	wf, err := wfRepo.FindByTaskID(context.Background(), tk.ID())
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusRouted, wf.Status)
}

func TestRouteTask_TransitionsExistingWorkflowRunToRouted(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}

	// Pre-create a WorkflowRun in created state
	existingWf := fixtures.NewTestWorkflowRun(fixtures.WithWorkflowTaskID(tk.ID()))
	wfRepo := &mockWfRepo{wfs: map[shared.TaskID]*workflow.WorkflowRun{tk.ID(): existingWf}}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	_, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)

	wf, err := wfRepo.FindByTaskID(context.Background(), tk.ID())
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusRouted, wf.Status)
	assert.NotNil(t, wf.RoutingDecisionID)
}

func TestRouteTask_RecordsAuditEntry(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	_, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.Len(t, auditRec.calls, 1)
	assert.Equal(t, shared.ActorID("system"), auditRec.calls[0].actor)
	assert.Equal(t, "task_routed", auditRec.calls[0].action)
	assert.NotEmpty(t, auditRec.calls[0].outcome)
	assert.NotNil(t, auditRec.calls[0].taskID)
}

func TestRouteTask_WorksWithNilMemoryProvider(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	rd, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.NotNil(t, rd)
	assert.Equal(t, tk.ID(), rd.TaskID())
}

func TestRouteTask_WorksWhenMemoryReturnsError(t *testing.T) {
	tk := fixtures.NewTestTask()
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}
	memProvider := &mockMemoryProvider{err: fmt.Errorf("memory unavailable")}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, memProvider)
	rd, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.NotNil(t, rd)
}

func TestRouteTask_FailsWhenTaskNotFound(t *testing.T) {
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	unknownID, _ := shared.NewTaskID(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
	rd, err := svc.RouteTask(context.Background(), unknownID)

	require.Error(t, err)
	assert.Nil(t, rd)
	assert.Contains(t, err.Error(), "task not found")
}

func TestRouteTask_CriticalRiskEscalates(t *testing.T) {
	tk := fixtures.NewTestTask(fixtures.WithTaskRiskLevel(shared.RiskLevelCritical))
	taskRepo := &mockTaskRepo{tasks: map[shared.TaskID]*task.Task{tk.ID(): tk}}
	routingRepo := &mockRoutingRepo{}
	wfRepo := &mockWfRepo{}
	idGen := &mockIDGen{}
	clock := newMockClock()
	auditRec := &mockAuditRecorder{}

	svc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, idGen, clock, auditRec, nil)
	rd, err := svc.RouteTask(context.Background(), tk.ID())

	require.NoError(t, err)
	require.NotNil(t, rd)
	assert.Equal(t, domainrouting.StrategyEscalate, rd.SelectedStrategy())
	assert.Equal(t, domainrouting.RoleHuman, rd.SelectedAgentRole())
}
