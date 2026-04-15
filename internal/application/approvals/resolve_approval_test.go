package approvals

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
)

var errNotFound = errors.New("not found")

// --- mocks ---

type mockApprovalRepo struct {
	approvals map[shared.ApprovalRequestID]*approval.ApprovalRequest
}

func (m *mockApprovalRepo) Save(_ context.Context, req *approval.ApprovalRequest) error {
	m.approvals[req.ID()] = req
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

type mockWfRepo struct {
	workflows map[shared.WorkflowRunID]*workflow.WorkflowRun
}

func (m *mockWfRepo) Save(_ context.Context, wf *workflow.WorkflowRun) error {
	m.workflows[wf.ID] = wf
	return nil
}

func (m *mockWfRepo) FindByID(_ context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	wf, ok := m.workflows[id]
	if !ok {
		return nil, errNotFound
	}
	return wf, nil
}

func (m *mockWfRepo) FindByTaskID(_ context.Context, _ shared.TaskID) (*workflow.WorkflowRun, error) {
	return nil, errNotFound
}

func (m *mockWfRepo) Update(_ context.Context, wf *workflow.WorkflowRun) error {
	m.workflows[wf.ID] = wf
	return nil
}

type mockLeaseRepo struct {
	leases map[shared.WorkflowRunID]*execution.ExecutionLease
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

type mockIDGen struct{ counter int }

func (m *mockIDGen) nextULID() string {
	m.counter++
	base := "01JTEST00000000000000000"
	suffix := string(rune('A'+m.counter-1)) + "0"
	return base[:24] + suffix
}

func (m *mockIDGen) NewTaskID() shared.TaskID                           { return shared.TaskID(m.nextULID()) }
func (m *mockIDGen) NewWorkflowRunID() shared.WorkflowRunID             { return shared.WorkflowRunID(m.nextULID()) }
func (m *mockIDGen) NewRoutingDecisionID() shared.RoutingDecisionID     { return shared.RoutingDecisionID(m.nextULID()) }
func (m *mockIDGen) NewPolicyDecisionID() shared.PolicyDecisionID       { return shared.PolicyDecisionID(m.nextULID()) }
func (m *mockIDGen) NewApprovalRequestID() shared.ApprovalRequestID     { return shared.ApprovalRequestID(m.nextULID()) }
func (m *mockIDGen) NewExecutionLeaseID() shared.ExecutionLeaseID       { return shared.ExecutionLeaseID(m.nextULID()) }
func (m *mockIDGen) NewEscalationTriggerID() shared.EscalationTriggerID { return shared.EscalationTriggerID(m.nextULID()) }
func (m *mockIDGen) NewAuditEntryID() shared.AuditEntryID               { return shared.AuditEntryID(m.nextULID()) }

type mockClock struct{ now time.Time }

func (m *mockClock) Now() shared.Timestamp { return shared.MustTimestamp(m.now) }

type auditRecord struct {
	Action  string
	Outcome string
}

type mockAuditRecorder struct{ records []auditRecord }

func (m *mockAuditRecorder) Record(_ context.Context, _ shared.ActorID, action, outcome string, _ audit.AuditContext, _ *shared.TaskID, _ *shared.WorkflowRunID) error {
	m.records = append(m.records, auditRecord{Action: action, Outcome: outcome})
	return nil
}

type mockNotifier struct {
	executionReadyCalls int
	terminatedCalls     int
}

func (m *mockNotifier) OnExecutionReady(_ context.Context, _ *workflow.WorkflowRun, _ *execution.ExecutionLease) error {
	m.executionReadyCalls++
	return nil
}

func (m *mockNotifier) OnApprovalRequired(_ context.Context, _ *workflow.WorkflowRun, _ *approval.ApprovalRequest) error {
	return nil
}

func (m *mockNotifier) OnWorkflowTerminated(_ context.Context, _ *workflow.WorkflowRun, _ string) error {
	m.terminatedCalls++
	return nil
}

// --- test harness ---

type testHarness struct {
	svc          *ApprovalService
	approvalRepo *mockApprovalRepo
	wfRepo       *mockWfRepo
	leaseRepo    *mockLeaseRepo
	idGen        *mockIDGen
	clock        *mockClock
	audit        *mockAuditRecorder
	notifier     *mockNotifier
}

func newTestHarness() *testHarness {
	h := &testHarness{
		approvalRepo: &mockApprovalRepo{approvals: make(map[shared.ApprovalRequestID]*approval.ApprovalRequest)},
		wfRepo:       &mockWfRepo{workflows: make(map[shared.WorkflowRunID]*workflow.WorkflowRun)},
		leaseRepo:    &mockLeaseRepo{leases: make(map[shared.WorkflowRunID]*execution.ExecutionLease)},
		idGen:        &mockIDGen{},
		clock:        &mockClock{now: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)},
		audit:        &mockAuditRecorder{},
		notifier:     &mockNotifier{},
	}
	h.svc = NewApprovalService(h.approvalRepo, h.wfRepo, h.leaseRepo, h.idGen, h.clock, h.audit, h.notifier)
	return h
}

// seedAwaitingApproval creates a workflow in awaiting_approval state and a pending ApprovalRequest.
func (h *testHarness) seedAwaitingApproval(t *testing.T) (*approval.ApprovalRequest, *workflow.WorkflowRun) {
	t.Helper()
	ctx := context.Background()
	now := h.clock.Now()
	actor := shared.ActorID("test-system")

	taskID := h.idGen.NewTaskID()
	wfID := h.idGen.NewWorkflowRunID()
	rdID := h.idGen.NewRoutingDecisionID()
	pdID := h.idGen.NewPolicyDecisionID()

	wf := workflow.NewWorkflowRun(wfID, taskID, now)
	require.NoError(t, wf.MarkRouted(rdID, "routed", actor, now))
	require.NoError(t, wf.MarkPolicyChecked(pdID, actor, now))
	require.NoError(t, wf.TransitionTo(workflow.StatusAwaitingApproval, "approval required", actor, now))
	require.NoError(t, h.wfRepo.Save(ctx, wf))

	ar, err := approval.NewApprovalRequest(
		h.idGen.NewApprovalRequestID(), taskID, wfID,
		"needs human approval", approval.ApproverSpec{Role: "human"}, nil, now,
	)
	require.NoError(t, err)
	require.NoError(t, h.approvalRepo.Save(ctx, ar))

	return ar, wf
}

// --- Tests ---

func TestResolveApproval_Approve_WorkflowRunning(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	ar, wf := h.seedAwaitingApproval(t)

	result, err := h.svc.ResolveApproval(ctx, inbound.ResolveApprovalInput{
		ApprovalRequestID: ar.ID(),
		Approved:          true,
		ResolvedBy:        shared.ActorID("reviewer"),
		Reason:            "looks good",
	})
	require.NoError(t, err)
	assert.Equal(t, approval.StatusApproved, result.Status())

	// Workflow should be running
	updated, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusRunning, updated.Status)

	// Lease should be created
	lease, err := h.leaseRepo.FindByWorkflowRunID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, execution.LeaseStatusActive, lease.Status)

	// Notifier
	assert.Equal(t, 1, h.notifier.executionReadyCalls)
	assert.Equal(t, 0, h.notifier.terminatedCalls)

	// Audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "approval_resolved", h.audit.records[0].Action)
	assert.Equal(t, "approved", h.audit.records[0].Outcome)
}

func TestResolveApproval_Deny_WorkflowFailed(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	ar, wf := h.seedAwaitingApproval(t)

	result, err := h.svc.ResolveApproval(ctx, inbound.ResolveApprovalInput{
		ApprovalRequestID: ar.ID(),
		Approved:          false,
		ResolvedBy:        shared.ActorID("reviewer"),
		Reason:            "too risky",
	})
	require.NoError(t, err)
	assert.Equal(t, approval.StatusDenied, result.Status())

	// Workflow should be failed
	updated, err := h.wfRepo.FindByID(ctx, wf.ID)
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusFailed, updated.Status)

	// Notifier
	assert.Equal(t, 0, h.notifier.executionReadyCalls)
	assert.Equal(t, 1, h.notifier.terminatedCalls)

	// Audit
	require.Len(t, h.audit.records, 1)
	assert.Equal(t, "denied", h.audit.records[0].Outcome)
}

func TestResolveApproval_NonPending_Error(t *testing.T) {
	h := newTestHarness()
	ctx := context.Background()

	ar, _ := h.seedAwaitingApproval(t)

	// Resolve once (approve)
	_, err := h.svc.ResolveApproval(ctx, inbound.ResolveApprovalInput{
		ApprovalRequestID: ar.ID(),
		Approved:          true,
		ResolvedBy:        shared.ActorID("reviewer"),
		Reason:            "ok",
	})
	require.NoError(t, err)

	// Attempt to resolve again — should fail because already approved
	_, err = h.svc.ResolveApproval(ctx, inbound.ResolveApprovalInput{
		ApprovalRequestID: ar.ID(),
		Approved:          false,
		ResolvedBy:        shared.ActorID("reviewer"),
		Reason:            "changed mind",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in pending status")
}
