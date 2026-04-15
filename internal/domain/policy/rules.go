package policy

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
)

// DefaultRules returns the phase-1 policy rules in evaluation order.
func DefaultRules() []PolicyRule {
	return []PolicyRule{
		&riskCriticalRequiresApproval{},
		&deploymentRequiresApproval{},
		&systemScopeRequiresApproval{},
		&destructiveActionDeny{},
		&fileScopeLowRiskAllow{},
	}
}

// --- riskCriticalRequiresApproval ---

type riskCriticalRequiresApproval struct{}

func (r *riskCriticalRequiresApproval) ID() string {
	return "risk_critical_requires_approval"
}

func (r *riskCriticalRequiresApproval) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.RiskLevel() == shared.RiskLevelCritical {
		outcome := OutcomeRequireApproval
		return RuleEvaluation{
			RuleID:  r.ID(),
			Passed:  false,
			Outcome: &outcome,
			Reason:  "critical risk level requires approval",
		}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: nil, Reason: "risk level is not critical"}
}

// --- deploymentRequiresApproval ---

type deploymentRequiresApproval struct{}

func (r *deploymentRequiresApproval) ID() string {
	return "deployment_requires_approval"
}

func (r *deploymentRequiresApproval) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Type() == task.TypeDeployment {
		outcome := OutcomeRequireApproval
		return RuleEvaluation{
			RuleID:  r.ID(),
			Passed:  false,
			Outcome: &outcome,
			Reason:  "deployment tasks require approval",
		}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: nil, Reason: "task is not a deployment"}
}

// --- systemScopeRequiresApproval ---

type systemScopeRequiresApproval struct{}

func (r *systemScopeRequiresApproval) ID() string {
	return "system_scope_requires_approval"
}

func (r *systemScopeRequiresApproval) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Scope() == task.ScopeSystem {
		outcome := OutcomeRequireApproval
		return RuleEvaluation{
			RuleID:  r.ID(),
			Passed:  false,
			Outcome: &outcome,
			Reason:  "system scope requires approval",
		}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: nil, Reason: "task scope is not system"}
}

// --- destructiveActionDeny ---

type destructiveActionDeny struct{}

func (r *destructiveActionDeny) ID() string {
	return "destructive_action_deny"
}

func (r *destructiveActionDeny) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Action == "data_deletion" {
		outcome := OutcomeDeny
		return RuleEvaluation{
			RuleID:  r.ID(),
			Passed:  false,
			Outcome: &outcome,
			Reason:  "data deletion is denied",
		}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: nil, Reason: "action is not destructive"}
}

// --- fileScopeLowRiskAllow ---

type fileScopeLowRiskAllow struct{}

func (r *fileScopeLowRiskAllow) ID() string {
	return "file_scope_low_risk_allow"
}

func (r *fileScopeLowRiskAllow) Evaluate(ctx PolicyContext) RuleEvaluation {
	if ctx.Task.Scope() == task.ScopeFile && ctx.Task.RiskLevel() == shared.RiskLevelLow {
		outcome := OutcomeAllow
		return RuleEvaluation{
			RuleID:  r.ID(),
			Passed:  true,
			Outcome: &outcome,
			Reason:  "file scope with low risk is allowed",
		}
	}
	return RuleEvaluation{RuleID: r.ID(), Passed: true, Outcome: nil, Reason: "not file scope with low risk"}
}
