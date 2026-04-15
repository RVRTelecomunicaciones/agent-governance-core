package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// PolicyDecisionOption configures a test policy decision.
type PolicyDecisionOption func(*policyDecisionConfig)

type policyDecisionConfig struct {
	id                  shared.PolicyDecisionID
	taskID              shared.TaskID
	action              string
	outcome             policy.PolicyOutcome
	constraints         []policy.PolicyConstraint
	approvalRequirement *policy.ApprovalRequirement
}

func defaultPolicyDecisionConfig() policyDecisionConfig {
	id, _ := shared.NewPolicyDecisionID(newULID())
	taskID, _ := shared.NewTaskID(newULID())
	return policyDecisionConfig{
		id:      id,
		taskID:  taskID,
		action:  "file_write",
		outcome: policy.OutcomeAllow,
	}
}

// WithPolicyDecisionID sets the policy decision ID.
func WithPolicyDecisionID(id shared.PolicyDecisionID) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.id = id }
}

// WithPolicyTaskID sets the task ID for the policy decision.
func WithPolicyTaskID(id shared.TaskID) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.taskID = id }
}

// WithPolicyOutcome sets the policy outcome.
func WithPolicyOutcome(o policy.PolicyOutcome) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.outcome = o }
}

// WithPolicyAction sets the evaluated action.
func WithPolicyAction(action string) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.action = action }
}

// WithPolicyConstraints sets the policy constraints.
func WithPolicyConstraints(cs []policy.PolicyConstraint) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.constraints = cs }
}

// WithApprovalRequirement sets the approval requirement.
func WithApprovalRequirement(ar *policy.ApprovalRequirement) PolicyDecisionOption {
	return func(c *policyDecisionConfig) { c.approvalRequirement = ar }
}

// NewTestPolicyDecision creates a valid PolicyDecision. Panics on error (test-only).
//
// Invariant auto-fill:
//   - If outcome is allow_with_constraints and no constraints provided, a default constraint is added.
//   - If outcome is require_approval and no approval requirement provided, a default is added.
func NewTestPolicyDecision(opts ...PolicyDecisionOption) *policy.PolicyDecision {
	cfg := defaultPolicyDecisionConfig()
	for _, o := range opts {
		o(&cfg)
	}

	// Auto-fill invariants based on outcome
	if cfg.outcome == policy.OutcomeAllowWithConstraints && len(cfg.constraints) == 0 {
		cfg.constraints = []policy.PolicyConstraint{
			{Type: "scope", Value: "read_only", Reason: "default test constraint"},
		}
	}
	if cfg.outcome == policy.OutcomeRequireApproval && cfg.approvalRequirement == nil {
		cfg.approvalRequirement = &policy.ApprovalRequirement{
			ApproverRole: "human",
			Reason:       "default test approval",
		}
	}

	allowOutcome := policy.OutcomeAllow
	rulesEvaluated := []policy.RuleEvaluation{
		{RuleID: "test-rule-1", Passed: true, Outcome: &allowOutcome, Reason: "test rule passed"},
	}

	pd, err := policy.NewPolicyDecision(
		cfg.id,
		cfg.taskID,
		cfg.action,
		cfg.outcome,
		cfg.constraints,
		cfg.approvalRequirement,
		rulesEvaluated,
		"test policy",
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestPolicyDecision: " + err.Error())
	}
	return pd
}
