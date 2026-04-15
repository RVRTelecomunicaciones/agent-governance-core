package escalation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	escalationdomain "github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// --- mocks ---

type mockEscalationRepo struct {
	saved []*escalationdomain.EscalationTrigger
}

func (m *mockEscalationRepo) Save(_ context.Context, trigger *escalationdomain.EscalationTrigger) error {
	m.saved = append(m.saved, trigger)
	return nil
}

func (m *mockEscalationRepo) FindByTaskID(_ context.Context, _ shared.TaskID) ([]*escalationdomain.EscalationTrigger, error) {
	return nil, nil
}

func (m *mockEscalationRepo) Update(_ context.Context, _ *escalationdomain.EscalationTrigger) error {
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

// --- Tests ---

func TestTriggerEscalation_CreatesTriggersPersistsAudits(t *testing.T) {
	repo := &mockEscalationRepo{}
	idGen := &mockIDGen{}
	clock := &mockClock{now: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)}
	auditRec := &mockAuditRecorder{}

	svc := NewEscalationService(repo, idGen, clock, auditRec)

	taskID := idGen.NewTaskID()
	condition := escalationdomain.EscalationCondition{
		Type:       "retries_exhausted",
		Parameters: map[string]any{"max_retries": 3},
	}

	trigger, err := svc.TriggerEscalation(context.Background(), taskID, condition, escalationdomain.TargetHuman)
	require.NoError(t, err)

	// Verify trigger state
	assert.Equal(t, escalationdomain.StatusTriggered, trigger.Status())
	assert.NotNil(t, trigger.TriggeredAt())
	assert.Equal(t, escalationdomain.TargetHuman, trigger.Target())
	assert.Equal(t, taskID, trigger.TaskID())

	// Verify persisted
	require.Len(t, repo.saved, 1)
	assert.Equal(t, trigger.ID(), repo.saved[0].ID())

	// Verify audit
	require.Len(t, auditRec.records, 1)
	assert.Equal(t, "escalation_triggered", auditRec.records[0].Action)
	assert.Equal(t, "triggered", auditRec.records[0].Outcome)
}
