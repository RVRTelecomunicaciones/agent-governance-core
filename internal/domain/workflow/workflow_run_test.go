package workflow

import (
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTimestamp(offset int) shared.Timestamp {
	return shared.MustTimestamp(time.Date(2026, 1, 1, 0, 0, offset, 0, time.UTC))
}

func testActor() shared.ActorID {
	id, _ := shared.NewActorID("system")
	return id
}

func testWorkflowRunID() shared.WorkflowRunID {
	id, _ := shared.NewWorkflowRunID("01JSGBAT6HQXF3Y8PZN4RK0001")
	return id
}

func testTaskID() shared.TaskID {
	id, _ := shared.NewTaskID("01JSGBAT6HQXF3Y8PZN4RK0002")
	return id
}

func testRoutingDecisionID() shared.RoutingDecisionID {
	id, _ := shared.NewRoutingDecisionID("01JSGBAT6HQXF3Y8PZN4RK0003")
	return id
}

func testPolicyDecisionID() shared.PolicyDecisionID {
	id, _ := shared.NewPolicyDecisionID("01JSGBAT6HQXF3Y8PZN4RK0004")
	return id
}

func TestNewWorkflowRun_StartsInCreated(t *testing.T) {
	now := testTimestamp(0)
	wf := NewWorkflowRun(testWorkflowRunID(), testTaskID(), now)

	assert.Equal(t, StatusCreated, wf.Status)
	assert.Empty(t, wf.Transitions)
	assert.Nil(t, wf.RoutingDecisionID)
	assert.Nil(t, wf.PolicyDecisionID)
	assert.Equal(t, 0, wf.CurrentStepIndex)
	assert.Equal(t, now, wf.CreatedAt)
	assert.Equal(t, now, wf.UpdatedAt)
}

func TestIsTerminal(t *testing.T) {
	terminal := []WorkflowStatus{StatusCompleted, StatusFailed, StatusKilled, StatusQuarantined}
	for _, s := range terminal {
		assert.True(t, s.IsTerminal(), "expected %s to be terminal", s)
	}

	nonTerminal := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusPolicyChecked,
		StatusRunning, StatusAwaitingApproval, StatusApproved, StatusPaused,
	}
	for _, s := range nonTerminal {
		assert.False(t, s.IsTerminal(), "expected %s to be non-terminal", s)
	}
}

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		from WorkflowStatus
		to   WorkflowStatus
	}{
		{StatusCreated, StatusRouted},
		{StatusRouted, StatusPolicyChecked},
		{StatusPolicyChecked, StatusRunning},
		{StatusPolicyChecked, StatusAwaitingApproval},
		{StatusPolicyChecked, StatusFailed},
		{StatusAwaitingApproval, StatusApproved},
		{StatusAwaitingApproval, StatusFailed},
		{StatusApproved, StatusRunning},
		{StatusRunning, StatusCompleted},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusPaused},
		{StatusRunning, StatusKilled},
		{StatusRunning, StatusQuarantined},
		{StatusPaused, StatusRunning},
		{StatusPaused, StatusKilled},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			assert.True(t, IsValidTransition(tt.from, tt.to))
		})
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		from WorkflowStatus
		to   WorkflowStatus
	}{
		{StatusCreated, StatusRunning},
		{StatusCreated, StatusCompleted},
		{StatusCreated, StatusPolicyChecked},
		{StatusRouted, StatusRunning},
		{StatusRouted, StatusCreated},
		{StatusPolicyChecked, StatusRouted},
		{StatusPolicyChecked, StatusCompleted},
		{StatusAwaitingApproval, StatusRunning}, // must go through approved first
		{StatusAwaitingApproval, StatusCompleted},
		{StatusApproved, StatusCompleted},
		{StatusApproved, StatusPaused},
		{StatusRunning, StatusRouted},
		{StatusRunning, StatusApproved},
		{StatusPaused, StatusCompleted},
		{StatusPaused, StatusFailed},
		// Terminal states cannot transition to anything
		{StatusCompleted, StatusRunning},
		{StatusCompleted, StatusKilled},
		{StatusFailed, StatusRunning},
		{StatusFailed, StatusKilled},
		{StatusKilled, StatusRunning},
		{StatusKilled, StatusCreated},
		{StatusQuarantined, StatusRunning},
		{StatusQuarantined, StatusCreated},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"→"+string(tt.to), func(t *testing.T) {
			assert.False(t, IsValidTransition(tt.from, tt.to))
		})
	}
}

func TestTransitionTo_ValidTransition(t *testing.T) {
	wf := NewWorkflowRun(testWorkflowRunID(), testTaskID(), testTimestamp(0))
	actor := testActor()

	// created → routed via MarkRouted
	err := wf.MarkRouted(testRoutingDecisionID(), "route reason", actor, testTimestamp(1))
	require.NoError(t, err)
	assert.Equal(t, StatusRouted, wf.Status)

	// routed → policy_checked via MarkPolicyChecked
	err = wf.MarkPolicyChecked(testPolicyDecisionID(), actor, testTimestamp(2))
	require.NoError(t, err)
	assert.Equal(t, StatusPolicyChecked, wf.Status)

	// policy_checked → running
	err = wf.TransitionTo(StatusRunning, "start", actor, testTimestamp(3))
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, wf.Status)

	// running → completed
	err = wf.Complete("done", actor, testTimestamp(4))
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, wf.Status)

	assert.Len(t, wf.Transitions, 4)
}

func TestTransitionTo_InvalidTransition_StatusUnchanged(t *testing.T) {
	wf := NewWorkflowRun(testWorkflowRunID(), testTaskID(), testTimestamp(0))
	actor := testActor()

	err := wf.TransitionTo(StatusRunning, "skip", actor, testTimestamp(1))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTransition)
	assert.Equal(t, StatusCreated, wf.Status)
	assert.Empty(t, wf.Transitions)
}

func TestKill_FromEveryNonTerminalState(t *testing.T) {
	nonTerminal := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusPolicyChecked,
		StatusRunning, StatusAwaitingApproval, StatusApproved, StatusPaused,
	}
	for _, status := range nonTerminal {
		t.Run("kill_from_"+string(status), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), status,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.Kill("killed", testActor(), testTimestamp(1))
			require.NoError(t, err)
			assert.Equal(t, StatusKilled, wf.Status)
		})
	}
}

func TestKill_FromTerminalState_Fails(t *testing.T) {
	terminal := []WorkflowStatus{StatusCompleted, StatusFailed, StatusKilled, StatusQuarantined}
	for _, status := range terminal {
		t.Run("kill_from_"+string(status), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), status,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.Kill("kill attempt", testActor(), testTimestamp(1))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrTerminalState)
			assert.Equal(t, status, wf.Status)
		})
	}
}

func TestNoTransitionFromTerminalStates(t *testing.T) {
	terminal := []WorkflowStatus{StatusCompleted, StatusFailed, StatusKilled, StatusQuarantined}
	targets := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusPolicyChecked,
		StatusRunning, StatusAwaitingApproval, StatusApproved,
		StatusPaused, StatusCompleted, StatusFailed, StatusKilled, StatusQuarantined,
	}
	for _, from := range terminal {
		for _, to := range targets {
			t.Run(string(from)+"→"+string(to), func(t *testing.T) {
				wf := ReconstructWorkflowRun(
					testWorkflowRunID(), testTaskID(), from,
					nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
				)
				err := wf.TransitionTo(to, "attempt", testActor(), testTimestamp(1))
				require.Error(t, err)
				assert.Equal(t, from, wf.Status)
			})
		}
	}
}

func TestAwaitingApproval_CannotGoDirectlyToRunning(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusAwaitingApproval,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.TransitionTo(StatusRunning, "skip approval", testActor(), testTimestamp(1))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidTransition)
	assert.Equal(t, StatusAwaitingApproval, wf.Status)
}

func TestTransitionsHistoryGrows(t *testing.T) {
	wf := NewWorkflowRun(testWorkflowRunID(), testTaskID(), testTimestamp(0))
	actor := testActor()

	assert.Len(t, wf.Transitions, 0)

	_ = wf.MarkRouted(testRoutingDecisionID(), "r", actor, testTimestamp(1))
	assert.Len(t, wf.Transitions, 1)
	assert.Equal(t, StatusCreated, wf.Transitions[0].From)
	assert.Equal(t, StatusRouted, wf.Transitions[0].To)

	_ = wf.MarkPolicyChecked(testPolicyDecisionID(), actor, testTimestamp(2))
	assert.Len(t, wf.Transitions, 2)

	_ = wf.TransitionTo(StatusRunning, "go", actor, testTimestamp(3))
	assert.Len(t, wf.Transitions, 3)

	_ = wf.Pause("wait", actor, testTimestamp(4))
	assert.Len(t, wf.Transitions, 4)

	_ = wf.Resume("continue", actor, testTimestamp(5))
	assert.Len(t, wf.Transitions, 5)

	_ = wf.Complete("finished", actor, testTimestamp(6))
	assert.Len(t, wf.Transitions, 6)
}

func TestMarkRouted_StoresRoutingDecisionID(t *testing.T) {
	wf := NewWorkflowRun(testWorkflowRunID(), testTaskID(), testTimestamp(0))
	rdID := testRoutingDecisionID()

	err := wf.MarkRouted(rdID, "routed", testActor(), testTimestamp(1))
	require.NoError(t, err)
	require.NotNil(t, wf.RoutingDecisionID)
	assert.Equal(t, rdID, *wf.RoutingDecisionID)
}

func TestMarkPolicyChecked_StoresPolicyDecisionID(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusRouted,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	pdID := testPolicyDecisionID()

	err := wf.MarkPolicyChecked(pdID, testActor(), testTimestamp(1))
	require.NoError(t, err)
	require.NotNil(t, wf.PolicyDecisionID)
	assert.Equal(t, pdID, *wf.PolicyDecisionID)
}

func TestPause_OnlyFromRunning(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusRunning,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.Pause("pause", testActor(), testTimestamp(1))
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, wf.Status)
}

func TestPause_FromNonRunning_Fails(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusCreated,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.Pause("pause", testActor(), testTimestamp(1))
	require.Error(t, err)
	assert.Equal(t, StatusCreated, wf.Status)
}

func TestResume_OnlyFromPaused(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusPaused,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.Resume("resume", testActor(), testTimestamp(1))
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, wf.Status)
}

func TestFail_FromValidStates(t *testing.T) {
	validForFail := []WorkflowStatus{
		StatusPolicyChecked, StatusAwaitingApproval, StatusRunning,
	}
	for _, status := range validForFail {
		t.Run("fail_from_"+string(status), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), status,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.Fail("error", testActor(), testTimestamp(1))
			require.NoError(t, err)
			assert.Equal(t, StatusFailed, wf.Status)
		})
	}
}

func TestFail_FromInvalidStates(t *testing.T) {
	invalidForFail := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusApproved, StatusPaused,
	}
	for _, status := range invalidForFail {
		t.Run("fail_from_"+string(status), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), status,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.Fail("error", testActor(), testTimestamp(1))
			require.Error(t, err)
			assert.Equal(t, status, wf.Status)
		})
	}
}

func TestReconstructWorkflowRun(t *testing.T) {
	rdID := testRoutingDecisionID()
	pdID := testPolicyDecisionID()
	transitions := []WorkflowTransition{
		{From: StatusCreated, To: StatusRouted, Reason: "r", Actor: testActor(), Timestamp: testTimestamp(1)},
	}

	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusRouted,
		&rdID, &pdID, 2, transitions, testTimestamp(0), testTimestamp(1),
	)

	assert.Equal(t, StatusRouted, wf.Status)
	assert.Equal(t, &rdID, wf.RoutingDecisionID)
	assert.Equal(t, &pdID, wf.PolicyDecisionID)
	assert.Equal(t, 2, wf.CurrentStepIndex)
	assert.Len(t, wf.Transitions, 1)
}

func TestWorkflowRun_Quarantine_FromRunning(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusRunning,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.Quarantine("budget exhausted", testActor(), testTimestamp(1))
	require.NoError(t, err)
	assert.Equal(t, StatusQuarantined, wf.Status)
	require.Len(t, wf.Transitions, 1)
	assert.Equal(t, StatusRunning, wf.Transitions[0].From)
	assert.Equal(t, StatusQuarantined, wf.Transitions[0].To)
	assert.Equal(t, "budget exhausted", wf.Transitions[0].Reason)
}

func TestWorkflowRun_Quarantined_IsTerminal(t *testing.T) {
	assert.True(t, StatusQuarantined.IsTerminal())
}

func TestWorkflowRun_NoTransitionFromQuarantined(t *testing.T) {
	targets := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusPolicyChecked,
		StatusRunning, StatusAwaitingApproval, StatusApproved,
		StatusPaused, StatusCompleted, StatusFailed, StatusKilled, StatusQuarantined,
	}
	for _, to := range targets {
		t.Run("quarantined→"+string(to), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), StatusQuarantined,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.TransitionTo(to, "attempt", testActor(), testTimestamp(1))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrTerminalState)
			assert.Equal(t, StatusQuarantined, wf.Status)
		})
	}
}

func TestWorkflowRun_KillFromQuarantined_Fails(t *testing.T) {
	wf := ReconstructWorkflowRun(
		testWorkflowRunID(), testTaskID(), StatusQuarantined,
		nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
	)
	err := wf.Kill("kill attempt", testActor(), testTimestamp(1))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTerminalState)
	assert.Equal(t, StatusQuarantined, wf.Status)
}

// TestSDD_DoneMapping_ApprovedIsNotTerminal verifies that StatusApproved is NOT
// the terminal "done" state for the SDD tasks phase. Spec #47 — IL2 requires
// tasks phase status "done", which maps to StatusCompleted at the workflow layer,
// NOT to StatusApproved (which is an intermediate human-approval gate).
func TestSDD_DoneMapping_ApprovedIsNotTerminal(t *testing.T) {
	assert.False(t, StatusApproved.IsTerminal(),
		"StatusApproved is an intermediate gate, not the SDD done equivalent")
}

// TestSDD_DoneMapping_CompletedIsTerminal verifies that StatusCompleted is the
// terminal success state, corresponding to SDD tasks phase "done". Spec #47.
func TestSDD_DoneMapping_CompletedIsTerminal(t *testing.T) {
	assert.True(t, StatusCompleted.IsTerminal(),
		"StatusCompleted is the terminal success state mapping to SDD tasks-phase done")
}

// TestSDD_DoneMapping_ApprovedCanTransitionToRunning verifies the approval flow:
// StatusApproved → StatusRunning (approved does NOT equal done; running must
// complete to reach the done/completed equivalent). Spec #47.
func TestSDD_DoneMapping_ApprovedCanTransitionToRunning(t *testing.T) {
	assert.True(t, IsValidTransition(StatusApproved, StatusRunning),
		"approved must be able to transition to running (approval gate, not done gate)")
	assert.False(t, IsValidTransition(StatusApproved, StatusCompleted),
		"approved cannot skip running to jump directly to completed")
}

func TestWorkflowRun_Quarantine_FromNonRunning_Fails(t *testing.T) {
	nonRunning := []WorkflowStatus{
		StatusCreated, StatusRouted, StatusPolicyChecked,
		StatusAwaitingApproval, StatusApproved, StatusPaused,
	}
	for _, status := range nonRunning {
		t.Run("quarantine_from_"+string(status), func(t *testing.T) {
			wf := ReconstructWorkflowRun(
				testWorkflowRunID(), testTaskID(), status,
				nil, nil, 0, nil, testTimestamp(0), testTimestamp(0),
			)
			err := wf.Quarantine("budget exhausted", testActor(), testTimestamp(1))
			require.Error(t, err)
			assert.Equal(t, status, wf.Status)
		})
	}
}
