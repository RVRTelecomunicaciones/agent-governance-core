package fixtures

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/escalation"
	"github.com/russellcxl/agent-governance-core/internal/domain/execution"
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
)

func TestTaskFactory_Defaults(t *testing.T) {
	tk := NewTestTask()
	assert.NotEmpty(t, tk.ID())
	assert.Equal(t, task.StatusCreated, tk.Status())
	assert.Equal(t, task.TypeDevelopment, tk.Type())
	assert.Equal(t, "Test task", tk.Title())
	assert.Equal(t, task.ScopeFile, tk.Scope())
}

func TestWorkflowRunFactory_Defaults(t *testing.T) {
	w := NewTestWorkflowRun()
	assert.NotEmpty(t, w.ID)
	assert.NotEmpty(t, w.TaskID)
	assert.Equal(t, workflow.StatusCreated, w.Status)
}

func TestRunningWorkflowRunFactory(t *testing.T) {
	w := NewRunningWorkflowRun()
	assert.NotEmpty(t, w.ID)
	assert.Equal(t, workflow.StatusRunning, w.Status)
	assert.NotNil(t, w.RoutingDecisionID)
	assert.NotNil(t, w.PolicyDecisionID)
	assert.Len(t, w.Transitions, 3) // created->routed, routed->policy_checked, policy_checked->running
}

func TestExecutionLeaseFactory_Defaults(t *testing.T) {
	lease := NewTestExecutionLease()
	assert.NotEmpty(t, lease.ID)
	assert.NotEmpty(t, lease.WorkflowRunID)
	assert.Equal(t, execution.LeaseStatusActive, lease.Status)
	assert.True(t, lease.HasRetryBudget())
}

func TestRoutingDecisionFactory_Defaults(t *testing.T) {
	rd := NewTestRoutingDecision()
	assert.NotEmpty(t, rd.ID())
	assert.NotEmpty(t, rd.TaskID())
	assert.Equal(t, "test routing", rd.Reason())
}

func TestPolicyDecisionFactory_Defaults(t *testing.T) {
	pd := NewTestPolicyDecision()
	assert.NotEmpty(t, pd.ID())
	assert.NotEmpty(t, pd.TaskID())
	assert.Equal(t, policy.OutcomeAllow, pd.Outcome())
	assert.Equal(t, "file_write", pd.EvaluatedAction())
}

func TestPolicyDecisionFactory_AllowWithConstraints(t *testing.T) {
	pd := NewTestPolicyDecision(WithPolicyOutcome(policy.OutcomeAllowWithConstraints))
	assert.Equal(t, policy.OutcomeAllowWithConstraints, pd.Outcome())
	assert.NotEmpty(t, pd.Constraints(), "should auto-fill default constraint")
}

func TestPolicyDecisionFactory_RequireApproval(t *testing.T) {
	pd := NewTestPolicyDecision(WithPolicyOutcome(policy.OutcomeRequireApproval))
	assert.Equal(t, policy.OutcomeRequireApproval, pd.Outcome())
	assert.NotNil(t, pd.ApprovalRequirement(), "should auto-fill default approval requirement")
}

func TestApprovalRequestFactory_Defaults(t *testing.T) {
	ar := NewTestApprovalRequest()
	assert.NotEmpty(t, ar.ID())
	assert.NotEmpty(t, ar.TaskID())
	assert.NotEmpty(t, ar.WorkflowRunID())
	assert.Equal(t, approval.StatusPending, ar.Status())
	assert.Equal(t, "test approval needed", ar.Reason())
}

func TestAuditEntryFactory_Defaults(t *testing.T) {
	entry := NewTestAuditEntry()
	assert.NotEmpty(t, entry.ID())
	assert.Equal(t, "test_action", entry.Action())
	assert.Equal(t, "success", entry.Outcome())
}

func TestEscalationTriggerFactory_Defaults(t *testing.T) {
	et := NewTestEscalationTrigger()
	assert.NotEmpty(t, et.ID())
	assert.NotEmpty(t, et.TaskID())
	assert.Equal(t, escalation.StatusPending, et.Status())
	assert.Equal(t, "retries_exhausted", et.Condition().Type)
	assert.Equal(t, escalation.TargetHuman, et.Target())
}
