package policy_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

func newTestTask(t *testing.T, taskType task.TaskType, scope task.TaskScope, riskLevel shared.RiskLevel) *task.Task {
	t.Helper()
	id, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Now())
	tk, err := task.NewTask(id, nil, taskType, "test task", scope, shared.PriorityNormal, riskLevel, nil, now)
	require.NoError(t, err)
	return tk
}

// --- Restrictiveness hierarchy tests ---

func TestPolicyOutcome_Restrictiveness(t *testing.T) {
	assert.Equal(t, 0, policy.OutcomeAllow.Restrictiveness())
	assert.Equal(t, 1, policy.OutcomeAllowWithConstraints.Restrictiveness())
	assert.Equal(t, 2, policy.OutcomeRequireApproval.Restrictiveness())
	assert.Equal(t, 3, policy.OutcomeDeny.Restrictiveness())
}

func TestPolicyOutcome_String(t *testing.T) {
	assert.Equal(t, "allow", policy.OutcomeAllow.String())
	assert.Equal(t, "allow_with_constraints", policy.OutcomeAllowWithConstraints.String())
	assert.Equal(t, "require_approval", policy.OutcomeRequireApproval.String())
	assert.Equal(t, "deny", policy.OutcomeDeny.String())
}

// --- Individual rule tests ---

func TestRule_CriticalRiskRequiresApproval(t *testing.T) {
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskLevelCritical)
	ctx := policy.PolicyContext{Task: tk, Action: "code_edit"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
	assert.NotNil(t, result.ApprovalRequirement)
	assert.Equal(t, "human", result.ApprovalRequirement.ApproverRole)
}

func TestRule_DeploymentRequiresApproval(t *testing.T) {
	tk := newTestTask(t, task.TypeDeployment, task.ScopeRepo, shared.RiskLevelMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "deploy"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
	assert.NotNil(t, result.ApprovalRequirement)
}

func TestRule_SystemScopeRequiresApproval(t *testing.T) {
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeSystem, shared.RiskLevelMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "config_change"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
	assert.NotNil(t, result.ApprovalRequirement)
}

func TestRule_DestructiveActionDeny(t *testing.T) {
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskLevelLow)
	ctx := policy.PolicyContext{Task: tk, Action: "data_deletion"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeDeny, result.Outcome)
}

func TestRule_FileScopeLowRiskAllow(t *testing.T) {
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskLevelLow)
	ctx := policy.PolicyContext{Task: tk, Action: "code_edit"}

	// Use only the file_scope_low_risk_allow rule to get a pure allow
	rules := policy.DefaultRules()
	// Filter to just the last rule
	result := policy.EvaluatePolicy([]policy.PolicyRule{rules[4]}, ctx)

	// Even with allow outcome from rule, evaluator defaults to allow_with_constraints
	// because allow alone triggers the default behavior
	assert.Equal(t, policy.OutcomeAllowWithConstraints, result.Outcome)
}

// --- Combination tests ---

func TestRule_DenyBeatRequireApproval(t *testing.T) {
	// Critical risk (require_approval) + data_deletion (deny) = deny wins
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskLevelCritical)
	ctx := policy.PolicyContext{Task: tk, Action: "data_deletion"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeDeny, result.Outcome)
	// No approval requirement when denied
	assert.Nil(t, result.ApprovalRequirement)
}

func TestRule_DefaultWhenNoRuleMatches(t *testing.T) {
	// Medium risk, module scope, non-destructive action — no rule produces explicit outcome
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeModule, shared.RiskLevelMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "code_edit"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeAllowWithConstraints, result.Outcome)
	require.NotEmpty(t, result.Constraints)
	assert.Equal(t, "timeout", result.Constraints[0].Type)
}

func TestRule_DeploymentRequiresApproval_NotJustCriticalRisk(t *testing.T) {
	// Deployment with low risk — deployment rule should still trigger
	tk := newTestTask(t, task.TypeDeployment, task.ScopeFile, shared.RiskLevelLow)
	ctx := policy.PolicyContext{Task: tk, Action: "deploy"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Equal(t, policy.OutcomeRequireApproval, result.Outcome)
	assert.NotNil(t, result.ApprovalRequirement)
}

// --- Sensitivity tests ---

func TestIsActionSensitive_True(t *testing.T) {
	assert.True(t, policy.IsActionSensitive("shell_execution"))
	assert.True(t, policy.IsActionSensitive("git_push"))
	assert.True(t, policy.IsActionSensitive("external_api_call"))
	assert.True(t, policy.IsActionSensitive("deployment"))
	assert.True(t, policy.IsActionSensitive("data_deletion"))
}

func TestIsActionSensitive_False(t *testing.T) {
	assert.False(t, policy.IsActionSensitive("file_read"))
	assert.False(t, policy.IsActionSensitive("code_edit"))
	assert.False(t, policy.IsActionSensitive(""))
}

// --- PolicyDecision validation tests ---

func TestPolicyDecision_RequireApprovalWithoutApprovalRequirement(t *testing.T) {
	id, err := shared.NewPolicyDecisionID(ulid.Make().String())
	require.NoError(t, err)
	taskID, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Now())

	_, err = policy.NewPolicyDecision(
		id, taskID, "some_action",
		policy.OutcomeRequireApproval,
		nil,
		nil, // missing approval requirement
		nil,
		"needs approval",
		now,
	)
	assert.ErrorIs(t, err, policy.ErrMissingApproval)
}

func TestPolicyDecision_AllowWithConstraintsWithoutConstraints(t *testing.T) {
	id, err := shared.NewPolicyDecisionID(ulid.Make().String())
	require.NoError(t, err)
	taskID, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Now())

	_, err = policy.NewPolicyDecision(
		id, taskID, "some_action",
		policy.OutcomeAllowWithConstraints,
		nil, // missing constraints
		nil,
		nil,
		"constrained",
		now,
	)
	assert.ErrorIs(t, err, policy.ErrMissingConstraints)
}

func TestPolicyDecision_EmptyAction(t *testing.T) {
	id, err := shared.NewPolicyDecisionID(ulid.Make().String())
	require.NoError(t, err)
	taskID, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Now())

	_, err = policy.NewPolicyDecision(
		id, taskID, "",
		policy.OutcomeAllow,
		nil, nil, nil,
		"reason",
		now,
	)
	assert.ErrorIs(t, err, policy.ErrEmptyAction)
}

func TestPolicyDecision_ValidCreation(t *testing.T) {
	id, err := shared.NewPolicyDecisionID(ulid.Make().String())
	require.NoError(t, err)
	taskID, err := shared.NewTaskID(ulid.Make().String())
	require.NoError(t, err)
	now := shared.MustTimestamp(time.Now())

	approval := &policy.ApprovalRequirement{ApproverRole: "human", Reason: "critical"}
	decision, err := policy.NewPolicyDecision(
		id, taskID, "deploy",
		policy.OutcomeRequireApproval,
		nil,
		approval,
		nil,
		"deployment requires approval",
		now,
	)
	require.NoError(t, err)
	assert.Equal(t, policy.OutcomeRequireApproval, decision.Outcome())
	assert.Equal(t, "deploy", decision.EvaluatedAction())
	assert.Equal(t, "human", decision.ApprovalRequirement().ApproverRole)
}

// --- DefaultRules count ---

func TestDefaultRules_Returns5Rules(t *testing.T) {
	rules := policy.DefaultRules()
	assert.Len(t, rules, 5)
}

// --- Evaluator all rules evaluated ---

func TestEvaluatePolicy_AllRulesEvaluated(t *testing.T) {
	tk := newTestTask(t, task.TypeDevelopment, task.ScopeModule, shared.RiskLevelMedium)
	ctx := policy.PolicyContext{Task: tk, Action: "code_edit"}

	result := policy.EvaluatePolicy(policy.DefaultRules(), ctx)

	assert.Len(t, result.RulesEvaluated, 5, "all 5 rules should be evaluated")
}
