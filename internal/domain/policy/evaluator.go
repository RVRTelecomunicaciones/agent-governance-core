package policy

// EvaluatorResult holds the aggregated result of running all policy rules.
type EvaluatorResult struct {
	Outcome             PolicyOutcome
	Constraints         []PolicyConstraint
	ApprovalRequirement *ApprovalRequirement
	RulesEvaluated      []RuleEvaluation
	Reason              string
}

// EvaluatePolicy runs all rules sequentially and returns the most restrictive outcome.
// If no rule produces an explicit outcome (or the most restrictive is just "allow"),
// it defaults to allow_with_constraints with a timeout constraint.
// If the outcome is require_approval, it auto-creates an ApprovalRequirement with role "human".
func EvaluatePolicy(rules []PolicyRule, ctx PolicyContext) EvaluatorResult {
	var (
		mostRestrictive = OutcomeAllow
		evals           []RuleEvaluation
		reason          string
	)

	for _, rule := range rules {
		eval := rule.Evaluate(ctx)
		evals = append(evals, eval)

		if eval.Outcome != nil && eval.Outcome.Restrictiveness() > mostRestrictive.Restrictiveness() {
			mostRestrictive = *eval.Outcome
			reason = eval.Reason
		}
	}

	// Default: if no rule matched or most restrictive is just allow, default to allow_with_constraints
	if mostRestrictive == OutcomeAllow {
		mostRestrictive = OutcomeAllowWithConstraints
		reason = "no rule explicitly allowed; defaulting to allow with timeout constraint"
	}

	result := EvaluatorResult{
		Outcome:        mostRestrictive,
		RulesEvaluated: evals,
		Reason:         reason,
	}

	switch mostRestrictive {
	case OutcomeAllowWithConstraints:
		defaultTimeout := 300
		result.Constraints = []PolicyConstraint{
			{Type: "timeout", Value: "300s", Reason: "default timeout constraint"},
		}
		_ = defaultTimeout
	case OutcomeRequireApproval:
		result.ApprovalRequirement = &ApprovalRequirement{
			ApproverRole: "human",
			Reason:       reason,
		}
	}

	return result
}
