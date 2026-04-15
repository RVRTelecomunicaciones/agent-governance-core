package policy

// PolicyOutcome represents the result of evaluating a policy rule.
type PolicyOutcome int

const (
	OutcomeAllow                PolicyOutcome = iota // 0
	OutcomeAllowWithConstraints                      // 1
	OutcomeRequireApproval                           // 2
	OutcomeDeny                                      // 3
)

var outcomeNames = map[PolicyOutcome]string{
	OutcomeAllow:                "allow",
	OutcomeAllowWithConstraints: "allow_with_constraints",
	OutcomeRequireApproval:      "require_approval",
	OutcomeDeny:                 "deny",
}

func (o PolicyOutcome) String() string {
	if name, ok := outcomeNames[o]; ok {
		return name
	}
	return "unknown"
}

// Restrictiveness returns the restrictiveness level of this outcome.
// Higher values are more restrictive: deny(3) > require_approval(2) > allow_with_constraints(1) > allow(0).
func (o PolicyOutcome) Restrictiveness() int {
	return int(o)
}

// PolicyConstraint is a value object representing a constraint on an allowed action.
type PolicyConstraint struct {
	Type   string
	Value  string
	Reason string
}

// ApprovalRequirement is a value object describing what approval is needed.
type ApprovalRequirement struct {
	ApproverRole string
	Reason       string
	TimeoutSecs  *int
}

// RuleEvaluation is a value object capturing the result of evaluating a single rule.
type RuleEvaluation struct {
	RuleID  string
	Passed  bool
	Outcome *PolicyOutcome // nil if rule didn't match
	Reason  string
}
