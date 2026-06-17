package intake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Inline mocks ---

type mockTaskRepo struct {
	saved *task.Task
}

func (m *mockTaskRepo) Save(_ context.Context, t *task.Task) error {
	m.saved = t
	return nil
}
func (m *mockTaskRepo) FindByID(_ context.Context, _ shared.TaskID) (*task.Task, error) {
	return m.saved, nil
}
func (m *mockTaskRepo) FindByParentID(_ context.Context, _ shared.TaskID) ([]*task.Task, error) {
	return nil, nil
}
func (m *mockTaskRepo) UpdateStatus(_ context.Context, _ *task.Task) error {
	return nil
}

type mockIDGen struct {
	taskID shared.TaskID
}

func (m *mockIDGen) NewTaskID() shared.TaskID                           { return m.taskID }
func (m *mockIDGen) NewWorkflowRunID() shared.WorkflowRunID             { return "" }
func (m *mockIDGen) NewRoutingDecisionID() shared.RoutingDecisionID     { return "" }
func (m *mockIDGen) NewPolicyDecisionID() shared.PolicyDecisionID       { return "" }
func (m *mockIDGen) NewApprovalRequestID() shared.ApprovalRequestID     { return "" }
func (m *mockIDGen) NewExecutionLeaseID() shared.ExecutionLeaseID       { return "" }
func (m *mockIDGen) NewEscalationTriggerID() shared.EscalationTriggerID { return "" }
func (m *mockIDGen) NewAuditEntryID() shared.AuditEntryID               { return "" }

type mockClock struct {
	now shared.Timestamp
}

func (m *mockClock) Now() shared.Timestamp { return m.now }

type mockAuditRecorder struct {
	recorded bool
	actor    shared.ActorID
	action   string
	outcome  string
	taskID   *shared.TaskID
}

func (m *mockAuditRecorder) Record(_ context.Context, actor shared.ActorID, action, outcome string, _ audit.AuditContext, taskID *shared.TaskID, _ *shared.WorkflowRunID) error {
	m.recorded = true
	m.actor = actor
	m.action = action
	m.outcome = outcome
	m.taskID = taskID
	return nil
}

type mockMemoryProvider struct {
	called bool
}

func (m *mockMemoryProvider) GetRelevantContext(_ context.Context, _ shared.TaskID, _ string) (*routing.MemoryContext, error) {
	m.called = true
	return nil, nil
}

// --- Helpers ---

// validTaskID returns a valid ULID string cast to TaskID for testing.
const testTaskIDStr = "01JSGW0000000000000000000A"

func newTestService(opts ...func(*SubmitTaskService)) (*SubmitTaskService, *mockTaskRepo, *mockAuditRecorder) {
	repo := &mockTaskRepo{}
	auditRec := &mockAuditRecorder{}
	svc := NewSubmitTaskService(
		repo,
		&mockIDGen{taskID: shared.TaskID(testTaskIDStr)},
		&mockClock{now: shared.MustTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))},
		auditRec,
		nil,
	)
	for _, o := range opts {
		o(svc)
	}
	return svc, repo, auditRec
}

func withMemory(mem *mockMemoryProvider) func(*SubmitTaskService) {
	return func(s *SubmitTaskService) { s.memory = mem }
}

// --- Tests ---

func TestSubmitTask_DevelopmentFile_RiskLow(t *testing.T) {
	svc, _, _ := newTestService()

	result, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeBugfix,
		Title:    "fix typo in readme",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityLow,
	})

	require.NoError(t, err)
	assert.Equal(t, shared.RiskLevelLow, result.RiskLevel())
}

func TestSubmitTask_DeploymentSystem_RiskCritical(t *testing.T) {
	svc, _, _ := newTestService()

	result, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDeployment,
		Title:    "deploy to production",
		Scope:    task.ScopeSystem,
		Priority: shared.PriorityUrgent,
	})

	require.NoError(t, err)
	assert.Equal(t, shared.RiskLevelCritical, result.RiskLevel())
}

func TestSubmitTask_EmptyTitle_Error(t *testing.T) {
	svc, _, _ := newTestService()

	_, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, task.ErrEmptyTitle)
}

func TestSubmitTask_PersistsTask(t *testing.T) {
	svc, repo, _ := newTestService()

	result, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "implement feature X",
		Scope:    task.ScopeModule,
		Priority: shared.PriorityNormal,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.saved)
	assert.Equal(t, result.ID(), repo.saved.ID())
}

func TestSubmitTask_RecordsAuditEntry(t *testing.T) {
	svc, _, auditRec := newTestService()

	result, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeReview,
		Title:    "review PR #42",
		Scope:    task.ScopeRepo,
		Priority: shared.PriorityNormal,
	})

	require.NoError(t, err)
	assert.True(t, auditRec.recorded)
	assert.Equal(t, shared.ActorID("system"), auditRec.actor)
	assert.Equal(t, "task_submitted", auditRec.action)
	assert.Equal(t, "created", auditRec.outcome)
	assert.Equal(t, result.ID(), *auditRec.taskID)
}

func TestSubmitTask_NilMemoryProvider_Degrades(t *testing.T) {
	svc, _, _ := newTestService() // memory is nil by default

	result, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "task without memory",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestSubmitTask_WithMemoryProvider_CallsIt(t *testing.T) {
	mem := &mockMemoryProvider{}
	svc, _, _ := newTestService(withMemory(mem))

	_, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "task with memory",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})

	require.NoError(t, err)
	assert.True(t, mem.called)
}

func TestSubmitTask_RepoSaveFails_ReturnsError(t *testing.T) {
	repo := &failingTaskRepo{err: errors.New("db down")}
	svc := NewSubmitTaskService(
		repo,
		&mockIDGen{taskID: shared.TaskID(testTaskIDStr)},
		&mockClock{now: shared.MustTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))},
		&mockAuditRecorder{},
		nil,
	)

	_, err := svc.SubmitTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "will fail persist",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "persisting task")
}

// --- Risk classification tests ---

func TestClassifyRisk(t *testing.T) {
	tests := []struct {
		name     string
		taskType task.TaskType
		scope    task.TaskScope
		want     shared.RiskLevel
	}{
		{"deployment+system=critical", task.TypeDeployment, task.ScopeSystem, shared.RiskLevelCritical},
		{"deployment+repo=high", task.TypeDeployment, task.ScopeRepo, shared.RiskLevelHigh},
		{"any+system=high", task.TypeDevelopment, task.ScopeSystem, shared.RiskLevelHigh},
		{"refactor+repo=medium", task.TypeRefactor, task.ScopeRepo, shared.RiskLevelMedium},
		{"bugfix+file=low", task.TypeBugfix, task.ScopeFile, shared.RiskLevelLow},
		{"development+module=medium(default)", task.TypeDevelopment, task.ScopeModule, shared.RiskLevelMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRisk(tt.taskType, tt.scope)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Failing mock for error paths ---

type failingTaskRepo struct {
	err error
}

func (m *failingTaskRepo) Save(_ context.Context, _ *task.Task) error {
	return m.err
}
func (m *failingTaskRepo) FindByID(_ context.Context, _ shared.TaskID) (*task.Task, error) {
	return nil, m.err
}
func (m *failingTaskRepo) FindByParentID(_ context.Context, _ shared.TaskID) ([]*task.Task, error) {
	return nil, m.err
}
func (m *failingTaskRepo) UpdateStatus(_ context.Context, _ *task.Task) error {
	return m.err
}
