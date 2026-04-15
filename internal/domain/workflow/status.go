package workflow

// WorkflowStatus represents the current state of a workflow run.
type WorkflowStatus string

const (
	StatusCreated          WorkflowStatus = "created"
	StatusRouted           WorkflowStatus = "routed"
	StatusPolicyChecked    WorkflowStatus = "policy_checked"
	StatusRunning          WorkflowStatus = "running"
	StatusAwaitingApproval WorkflowStatus = "awaiting_approval"
	StatusApproved         WorkflowStatus = "approved"
	StatusPaused           WorkflowStatus = "paused"
	StatusCompleted        WorkflowStatus = "completed"
	StatusFailed           WorkflowStatus = "failed"
	StatusKilled           WorkflowStatus = "killed"
)

var terminalStates = map[WorkflowStatus]bool{
	StatusCompleted: true,
	StatusFailed:    true,
	StatusKilled:    true,
}

// IsTerminal returns true if the status is a terminal state (completed, failed, killed).
func (s WorkflowStatus) IsTerminal() bool {
	return terminalStates[s]
}

// transitionTable defines all valid from → to transitions (excluding kill, which is special).
var transitionTable = map[WorkflowStatus]map[WorkflowStatus]bool{
	StatusCreated: {
		StatusRouted: true,
	},
	StatusRouted: {
		StatusPolicyChecked: true,
	},
	StatusPolicyChecked: {
		StatusRunning:          true,
		StatusAwaitingApproval: true,
		StatusFailed:           true,
	},
	StatusAwaitingApproval: {
		StatusApproved: true,
		StatusFailed:   true,
	},
	StatusApproved: {
		StatusRunning: true,
	},
	StatusRunning: {
		StatusCompleted: true,
		StatusFailed:    true,
		StatusPaused:    true,
		StatusKilled:    true,
	},
	StatusPaused: {
		StatusRunning: true,
		StatusKilled:  true,
	},
}

// IsValidTransition checks whether a transition from this status to target is allowed.
// Kill transitions from non-terminal states are always valid.
func IsValidTransition(from, to WorkflowStatus) bool {
	// Kill from any non-terminal state is always valid.
	if to == StatusKilled && !from.IsTerminal() {
		return true
	}

	targets, ok := transitionTable[from]
	if !ok {
		return false
	}
	return targets[to]
}
