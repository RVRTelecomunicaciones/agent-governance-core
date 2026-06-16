package fixtures

import (
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
)

// WorkflowRunOption configures a test workflow run.
type WorkflowRunOption func(*workflowRunConfig)

type workflowRunConfig struct {
	id     shared.WorkflowRunID
	taskID shared.TaskID
}

func defaultWorkflowRunConfig() workflowRunConfig {
	id, _ := shared.NewWorkflowRunID(newULID())
	taskID, _ := shared.NewTaskID(newULID())
	return workflowRunConfig{
		id:     id,
		taskID: taskID,
	}
}

// WithWorkflowRunID sets the workflow run ID.
func WithWorkflowRunID(id shared.WorkflowRunID) WorkflowRunOption {
	return func(c *workflowRunConfig) { c.id = id }
}

// WithWorkflowTaskID sets the task ID for the workflow run.
func WithWorkflowTaskID(id shared.TaskID) WorkflowRunOption {
	return func(c *workflowRunConfig) { c.taskID = id }
}

// NewTestWorkflowRun creates a valid WorkflowRun in "created" status.
func NewTestWorkflowRun(opts ...WorkflowRunOption) *workflow.WorkflowRun {
	cfg := defaultWorkflowRunConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return workflow.NewWorkflowRun(cfg.id, cfg.taskID, now())
}

// NewRunningWorkflowRun creates a WorkflowRun that has been transitioned through
// created -> routed -> policy_checked -> running, using fake routing/policy decision IDs.
// Panics on transition error (test-only).
func NewRunningWorkflowRun(opts ...WorkflowRunOption) *workflow.WorkflowRun {
	w := NewTestWorkflowRun(opts...)
	ts := now()

	actor, _ := shared.NewActorID("test-system")
	rdID, _ := shared.NewRoutingDecisionID(newULID())
	pdID, _ := shared.NewPolicyDecisionID(newULID())

	if err := w.MarkRouted(rdID, "test routing", actor, ts); err != nil {
		panic("fixtures.NewRunningWorkflowRun: MarkRouted: " + err.Error())
	}
	if err := w.MarkPolicyChecked(pdID, actor, ts); err != nil {
		panic("fixtures.NewRunningWorkflowRun: MarkPolicyChecked: " + err.Error())
	}
	if err := w.TransitionTo(workflow.StatusRunning, "test start", actor, ts); err != nil {
		panic("fixtures.NewRunningWorkflowRun: TransitionTo running: " + err.Error())
	}
	return w
}
