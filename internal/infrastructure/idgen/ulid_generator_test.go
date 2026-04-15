package idgen

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

func TestULIDGenerator_GeneratesValidULIDs(t *testing.T) {
	g := ULIDGenerator{}

	id := g.NewTaskID()
	_, err := ulid.ParseStrict(id.String())
	assert.NoError(t, err, "generated TaskID should be a valid ULID")
	assert.Len(t, id.String(), 26, "ULID should be 26 characters")
}

func TestULIDGenerator_Uniqueness(t *testing.T) {
	g := ULIDGenerator{}
	seen := make(map[string]bool, 100)

	for i := 0; i < 100; i++ {
		id := g.NewTaskID()
		s := id.String()
		require.False(t, seen[s], "duplicate ID generated: %s", s)
		seen[s] = true
	}
}

func TestULIDGenerator_AllMethods(t *testing.T) {
	g := ULIDGenerator{}

	tests := []struct {
		name string
		fn   func() string
	}{
		{"TaskID", func() string { return g.NewTaskID().String() }},
		{"WorkflowRunID", func() string { return g.NewWorkflowRunID().String() }},
		{"RoutingDecisionID", func() string { return g.NewRoutingDecisionID().String() }},
		{"PolicyDecisionID", func() string { return g.NewPolicyDecisionID().String() }},
		{"ApprovalRequestID", func() string { return g.NewApprovalRequestID().String() }},
		{"ExecutionLeaseID", func() string { return g.NewExecutionLeaseID().String() }},
		{"EscalationTriggerID", func() string { return g.NewEscalationTriggerID().String() }},
		{"AuditEntryID", func() string { return g.NewAuditEntryID().String() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.fn()
			assert.Len(t, s, 26)
			_, err := ulid.ParseStrict(s)
			assert.NoError(t, err)
		})
	}
}

func TestULIDGenerator_TypedReturns(t *testing.T) {
	g := ULIDGenerator{}

	// Compile-time type checks via assignments
	var _ shared.TaskID = g.NewTaskID()
	var _ shared.WorkflowRunID = g.NewWorkflowRunID()
	var _ shared.RoutingDecisionID = g.NewRoutingDecisionID()
	var _ shared.PolicyDecisionID = g.NewPolicyDecisionID()
	var _ shared.ApprovalRequestID = g.NewApprovalRequestID()
	var _ shared.ExecutionLeaseID = g.NewExecutionLeaseID()
	var _ shared.EscalationTriggerID = g.NewEscalationTriggerID()
	var _ shared.AuditEntryID = g.NewAuditEntryID()
}

func TestULIDGenerator_TimeSortable(t *testing.T) {
	g := ULIDGenerator{}

	first := g.NewTaskID()
	time.Sleep(2 * time.Millisecond)
	second := g.NewTaskID()

	assert.True(t, second.String() > first.String(),
		"second ID (%s) should be lexicographically greater than first (%s)", second, first)
}
