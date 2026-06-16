package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appaudit "github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/audit"
	domainaudit "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

// --- Mocks ---

type mockAuditRepo struct {
	appendedEntries []*domainaudit.AuditEntry
}

func (m *mockAuditRepo) Append(_ context.Context, e *domainaudit.AuditEntry) error {
	m.appendedEntries = append(m.appendedEntries, e)
	return nil
}

func (m *mockAuditRepo) Query(_ context.Context, _ outbound.AuditFilter) ([]*domainaudit.AuditEntry, int, error) {
	return nil, 0, nil
}

type mockIDGen struct {
	auditEntryID shared.AuditEntryID
}

func (m *mockIDGen) NewTaskID() shared.TaskID                       { return "" }
func (m *mockIDGen) NewWorkflowRunID() shared.WorkflowRunID         { return "" }
func (m *mockIDGen) NewRoutingDecisionID() shared.RoutingDecisionID { return "" }
func (m *mockIDGen) NewPolicyDecisionID() shared.PolicyDecisionID   { return "" }
func (m *mockIDGen) NewApprovalRequestID() shared.ApprovalRequestID { return "" }
func (m *mockIDGen) NewExecutionLeaseID() shared.ExecutionLeaseID   { return "" }
func (m *mockIDGen) NewEscalationTriggerID() shared.EscalationTriggerID {
	return ""
}
func (m *mockIDGen) NewAuditEntryID() shared.AuditEntryID { return m.auditEntryID }

type mockClock struct {
	now shared.Timestamp
}

func (m *mockClock) Now() shared.Timestamp { return m.now }

// --- Helpers ---

func fixedTime() shared.Timestamp {
	return shared.MustTimestamp(time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC))
}

func fixedAuditEntryID(t *testing.T) shared.AuditEntryID {
	t.Helper()
	id, err := shared.NewAuditEntryID("01KP7WTMTBF869Z7RD7EJNP360")
	require.NoError(t, err)
	return id
}

func newService(t *testing.T) (*appaudit.RecordAuditService, *mockAuditRepo) {
	t.Helper()
	repo := &mockAuditRepo{}
	idGen := &mockIDGen{auditEntryID: fixedAuditEntryID(t)}
	clock := &mockClock{now: fixedTime()}
	svc := appaudit.NewRecordAuditService(repo, idGen, clock)
	return svc, repo
}

// --- Tests ---

func TestRecord_CreatesAuditEntryWithCorrectFields(t *testing.T) {
	svc, repo := newService(t)
	actor, _ := shared.NewActorID("agent-1")
	auditCtx := domainaudit.NewAuditContext().WithToolName("file_read")

	err := svc.Record(context.Background(), actor, "tool_execution", "success", auditCtx, nil, nil)

	require.NoError(t, err)
	require.Len(t, repo.appendedEntries, 1)
	entry := repo.appendedEntries[0]
	assert.Equal(t, fixedAuditEntryID(t), entry.ID())
	assert.Equal(t, actor, entry.Actor())
	assert.Equal(t, "tool_execution", entry.Action())
	assert.Equal(t, "success", entry.Outcome())
	assert.Equal(t, fixedTime(), entry.CreatedAt())
}

func TestRecord_CallsAppendOnRepository(t *testing.T) {
	svc, repo := newService(t)
	actor, _ := shared.NewActorID("agent-1")
	auditCtx := domainaudit.NewAuditContext()

	err := svc.Record(context.Background(), actor, "routing", "direct", auditCtx, nil, nil)

	require.NoError(t, err)
	assert.Len(t, repo.appendedEntries, 1)
}

func TestRecord_SetsTaskIDWhenProvided(t *testing.T) {
	svc, repo := newService(t)
	actor, _ := shared.NewActorID("agent-1")
	auditCtx := domainaudit.NewAuditContext()
	taskID, err := shared.NewTaskID("01KP7WTMTBTRA4S2456P21NJZ0")
	require.NoError(t, err)

	err = svc.Record(context.Background(), actor, "action", "outcome", auditCtx, &taskID, nil)

	require.NoError(t, err)
	require.Len(t, repo.appendedEntries, 1)
	assert.NotNil(t, repo.appendedEntries[0].TaskID())
	assert.Equal(t, taskID, *repo.appendedEntries[0].TaskID())
}

func TestRecord_SetsWorkflowRunIDWhenProvided(t *testing.T) {
	svc, repo := newService(t)
	actor, _ := shared.NewActorID("agent-1")
	auditCtx := domainaudit.NewAuditContext()
	wfID, err := shared.NewWorkflowRunID("01KP7WTMTBE2DGEP087XGX6XDW")
	require.NoError(t, err)

	err = svc.Record(context.Background(), actor, "action", "outcome", auditCtx, nil, &wfID)

	require.NoError(t, err)
	require.Len(t, repo.appendedEntries, 1)
	assert.NotNil(t, repo.appendedEntries[0].WorkflowRunID())
	assert.Equal(t, wfID, *repo.appendedEntries[0].WorkflowRunID())
}

func TestRecord_CallsWithTraceInfo(t *testing.T) {
	svc, repo := newService(t)
	actor, _ := shared.NewActorID("agent-1")
	auditCtx := domainaudit.NewAuditContext()

	// Without OTel configured, WithTraceInfo should be a no-op (no panic, no trace keys)
	err := svc.Record(context.Background(), actor, "action", "outcome", auditCtx, nil, nil)

	require.NoError(t, err)
	require.Len(t, repo.appendedEntries, 1)
	// Context should not contain trace_id when no OTel span is active
	ctx := repo.appendedEntries[0].Context()
	_, hasTraceID := ctx["trace_id"]
	assert.False(t, hasTraceID, "no trace_id expected without active OTel span")
}
