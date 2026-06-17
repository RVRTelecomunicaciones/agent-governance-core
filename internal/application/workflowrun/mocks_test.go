package workflowrun

import (
	"context"
	"errors"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/escalation"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

// Ensure imports are used.
var (
	_ = escalation.TargetHuman
	_ = routing.StrategyDirect
)

var errNotFound = errors.New("not found")

// --- mockTaskRepo ---

type mockTaskRepo struct {
	tasks map[shared.TaskID]*task.Task
}

func newMockTaskRepo() *mockTaskRepo {
	return &mockTaskRepo{tasks: make(map[shared.TaskID]*task.Task)}
}

func (m *mockTaskRepo) Save(_ context.Context, t *task.Task) error {
	m.tasks[t.ID()] = t
	return nil
}

func (m *mockTaskRepo) FindByID(_ context.Context, id shared.TaskID) (*task.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return nil, errNotFound
	}
	return t, nil
}

func (m *mockTaskRepo) FindByParentID(_ context.Context, _ shared.TaskID) ([]*task.Task, error) {
	return nil, nil
}

func (m *mockTaskRepo) UpdateStatus(_ context.Context, _ *task.Task) error {
	return nil
}

// --- mockWfRepo ---

type mockWfRepo struct {
	workflows map[shared.WorkflowRunID]*workflow.WorkflowRun
	byTask    map[shared.TaskID]*workflow.WorkflowRun
}

func newMockWfRepo() *mockWfRepo {
	return &mockWfRepo{
		workflows: make(map[shared.WorkflowRunID]*workflow.WorkflowRun),
		byTask:    make(map[shared.TaskID]*workflow.WorkflowRun),
	}
}

func (m *mockWfRepo) Save(_ context.Context, wf *workflow.WorkflowRun) error {
	m.workflows[wf.ID] = wf
	m.byTask[wf.TaskID] = wf
	return nil
}

func (m *mockWfRepo) FindByID(_ context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	wf, ok := m.workflows[id]
	if !ok {
		return nil, errNotFound
	}
	return wf, nil
}

func (m *mockWfRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	wf, ok := m.byTask[taskID]
	if !ok {
		return nil, errNotFound
	}
	return wf, nil
}

func (m *mockWfRepo) Update(_ context.Context, wf *workflow.WorkflowRun) error {
	m.workflows[wf.ID] = wf
	m.byTask[wf.TaskID] = wf
	return nil
}

func (m *mockWfRepo) List(_ context.Context, _ outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}

// --- mockLeaseRepo ---

type mockLeaseRepo struct {
	leases map[shared.WorkflowRunID]*execution.ExecutionLease
}

func newMockLeaseRepo() *mockLeaseRepo {
	return &mockLeaseRepo{leases: make(map[shared.WorkflowRunID]*execution.ExecutionLease)}
}

func (m *mockLeaseRepo) Save(_ context.Context, lease *execution.ExecutionLease) error {
	m.leases[lease.WorkflowRunID] = lease
	return nil
}

func (m *mockLeaseRepo) FindByWorkflowRunID(_ context.Context, wfID shared.WorkflowRunID) (*execution.ExecutionLease, error) {
	l, ok := m.leases[wfID]
	if !ok {
		return nil, errNotFound
	}
	return l, nil
}

func (m *mockLeaseRepo) Update(_ context.Context, lease *execution.ExecutionLease) error {
	m.leases[lease.WorkflowRunID] = lease
	return nil
}

// --- mockRoutingRepo ---

type mockRoutingRepo struct {
	byTask map[shared.TaskID]*routing.RoutingDecision
}

func newMockRoutingRepo() *mockRoutingRepo {
	return &mockRoutingRepo{byTask: make(map[shared.TaskID]*routing.RoutingDecision)}
}

func (m *mockRoutingRepo) Save(_ context.Context, rd *routing.RoutingDecision) error {
	m.byTask[rd.TaskID()] = rd
	return nil
}

func (m *mockRoutingRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	rd, ok := m.byTask[taskID]
	if !ok {
		return nil, errNotFound
	}
	return rd, nil
}

// --- mockPolicyRepo ---

type mockPolicyRepo struct {
	byTask map[shared.TaskID]*policy.PolicyDecision
}

func newMockPolicyRepo() *mockPolicyRepo {
	return &mockPolicyRepo{byTask: make(map[shared.TaskID]*policy.PolicyDecision)}
}

func (m *mockPolicyRepo) Save(_ context.Context, pd *policy.PolicyDecision) error {
	m.byTask[pd.TaskID()] = pd
	return nil
}

func (m *mockPolicyRepo) FindByTaskID(_ context.Context, taskID shared.TaskID) (*policy.PolicyDecision, error) {
	pd, ok := m.byTask[taskID]
	if !ok {
		return nil, errNotFound
	}
	return pd, nil
}

// --- mockApprovalRepo ---

type mockApprovalRepo struct {
	approvals map[shared.ApprovalRequestID]*approval.ApprovalRequest
	saved     []*approval.ApprovalRequest
}

func newMockApprovalRepo() *mockApprovalRepo {
	return &mockApprovalRepo{approvals: make(map[shared.ApprovalRequestID]*approval.ApprovalRequest)}
}

func (m *mockApprovalRepo) Save(_ context.Context, req *approval.ApprovalRequest) error {
	m.approvals[req.ID()] = req
	m.saved = append(m.saved, req)
	return nil
}

func (m *mockApprovalRepo) FindByID(_ context.Context, id shared.ApprovalRequestID) (*approval.ApprovalRequest, error) {
	r, ok := m.approvals[id]
	if !ok {
		return nil, errNotFound
	}
	return r, nil
}

func (m *mockApprovalRepo) FindByTaskID(_ context.Context, _ shared.TaskID) ([]*approval.ApprovalRequest, error) {
	return nil, nil
}

func (m *mockApprovalRepo) FindPending(_ context.Context) ([]*approval.ApprovalRequest, error) {
	return nil, nil
}

func (m *mockApprovalRepo) Update(_ context.Context, req *approval.ApprovalRequest) error {
	m.approvals[req.ID()] = req
	return nil
}

// --- mockIDGen ---

type mockIDGen struct {
	counter int
}

func newMockIDGen() *mockIDGen {
	return &mockIDGen{}
}

func (m *mockIDGen) nextULID() string {
	m.counter++
	// Generate a deterministic ULID-like string (26 chars, valid ULID base32)
	base := "01JTEST00000000000000000"
	suffix := string(rune('A'+m.counter-1)) + "0"
	return base[:24] + suffix
}

func (m *mockIDGen) NewTaskID() shared.TaskID { return shared.TaskID(m.nextULID()) }
func (m *mockIDGen) NewWorkflowRunID() shared.WorkflowRunID {
	return shared.WorkflowRunID(m.nextULID())
}
func (m *mockIDGen) NewRoutingDecisionID() shared.RoutingDecisionID {
	return shared.RoutingDecisionID(m.nextULID())
}
func (m *mockIDGen) NewPolicyDecisionID() shared.PolicyDecisionID {
	return shared.PolicyDecisionID(m.nextULID())
}
func (m *mockIDGen) NewApprovalRequestID() shared.ApprovalRequestID {
	return shared.ApprovalRequestID(m.nextULID())
}
func (m *mockIDGen) NewExecutionLeaseID() shared.ExecutionLeaseID {
	return shared.ExecutionLeaseID(m.nextULID())
}
func (m *mockIDGen) NewEscalationTriggerID() shared.EscalationTriggerID {
	return shared.EscalationTriggerID(m.nextULID())
}
func (m *mockIDGen) NewAuditEntryID() shared.AuditEntryID { return shared.AuditEntryID(m.nextULID()) }

// --- mockClock ---

type mockClock struct {
	now time.Time
}

func newMockClock() *mockClock {
	return &mockClock{now: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)}
}

func (m *mockClock) Now() shared.Timestamp {
	return shared.MustTimestamp(m.now)
}

// --- mockAuditRecorder ---

type auditRecord struct {
	Actor   shared.ActorID
	Action  string
	Outcome string
	Ctx     audit.AuditContext
	TaskID  *shared.TaskID
	WfID    *shared.WorkflowRunID
}

type mockAuditRecorder struct {
	records []auditRecord
}

func newMockAuditRecorder() *mockAuditRecorder {
	return &mockAuditRecorder{}
}

func (m *mockAuditRecorder) Record(_ context.Context, actor shared.ActorID, action, outcome string, ctx audit.AuditContext, taskID *shared.TaskID, wfID *shared.WorkflowRunID) error {
	m.records = append(m.records, auditRecord{
		Actor:   actor,
		Action:  action,
		Outcome: outcome,
		Ctx:     ctx,
		TaskID:  taskID,
		WfID:    wfID,
	})
	return nil
}

// --- mockNotifier ---

type mockNotifier struct {
	executionReadyCalls   int
	approvalRequiredCalls int
	terminatedCalls       int
	lastTerminatedReason  string
}

func newMockNotifier() *mockNotifier {
	return &mockNotifier{}
}

func (m *mockNotifier) OnExecutionReady(_ context.Context, _ *workflow.WorkflowRun, _ *execution.ExecutionLease) error {
	m.executionReadyCalls++
	return nil
}

func (m *mockNotifier) OnApprovalRequired(_ context.Context, _ *workflow.WorkflowRun, _ *approval.ApprovalRequest) error {
	m.approvalRequiredCalls++
	return nil
}

func (m *mockNotifier) OnWorkflowTerminated(_ context.Context, _ *workflow.WorkflowRun, reason string) error {
	m.terminatedCalls++
	m.lastTerminatedReason = reason
	return nil
}
