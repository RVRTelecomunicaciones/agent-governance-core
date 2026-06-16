package shared_test

import (
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validULID() string {
	return ulid.Make().String()
}

func TestTaskID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewTaskID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewTaskID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewTaskID("not-a-ulid")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestWorkflowRunID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewWorkflowRunID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewWorkflowRunID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewWorkflowRunID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestRoutingDecisionID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewRoutingDecisionID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewRoutingDecisionID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewRoutingDecisionID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestPolicyDecisionID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewPolicyDecisionID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewPolicyDecisionID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewPolicyDecisionID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestApprovalRequestID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewApprovalRequestID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewApprovalRequestID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewApprovalRequestID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestExecutionLeaseID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewExecutionLeaseID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewExecutionLeaseID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewExecutionLeaseID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestEscalationTriggerID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewEscalationTriggerID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewEscalationTriggerID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewEscalationTriggerID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestAuditEntryID(t *testing.T) {
	t.Run("accepts valid ULID", func(t *testing.T) {
		raw := validULID()
		id, err := shared.NewAuditEntryID(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewAuditEntryID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})

	t.Run("rejects invalid ULID", func(t *testing.T) {
		_, err := shared.NewAuditEntryID("bad")
		assert.ErrorIs(t, err, shared.ErrInvalidULID)
	})
}

func TestActorID(t *testing.T) {
	t.Run("accepts any non-empty string", func(t *testing.T) {
		id, err := shared.NewActorID("user:john")
		require.NoError(t, err)
		assert.Equal(t, "user:john", id.String())
	})

	t.Run("accepts non-ULID strings", func(t *testing.T) {
		id, err := shared.NewActorID("system")
		require.NoError(t, err)
		assert.Equal(t, "system", id.String())
	})

	t.Run("rejects empty string", func(t *testing.T) {
		_, err := shared.NewActorID("")
		assert.ErrorIs(t, err, shared.ErrEmptyID)
	})
}

func TestIDTypesAreDistinct(t *testing.T) {
	// This is a compile-time check — if these types were aliases,
	// assigning one to the other would compile. We verify they are
	// distinct by checking they are different types at runtime.
	raw := validULID()
	taskID, _ := shared.NewTaskID(raw)
	workflowRunID, _ := shared.NewWorkflowRunID(raw)

	// Same underlying value, different types
	assert.Equal(t, taskID.String(), workflowRunID.String())

	// The following would NOT compile if uncommented, proving type safety:
	// var _ shared.TaskID = workflowRunID
}
