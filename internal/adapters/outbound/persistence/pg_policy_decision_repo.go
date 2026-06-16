package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ outbound.PolicyDecisionRepository = (*PgPolicyDecisionRepository)(nil)

// PgPolicyDecisionRepository implements PolicyDecisionRepository using PostgreSQL.
// PolicyDecision is immutable — no Update method.
type PgPolicyDecisionRepository struct {
	pool *pgxpool.Pool
}

// NewPgPolicyDecisionRepository creates a new PgPolicyDecisionRepository.
func NewPgPolicyDecisionRepository(pool *pgxpool.Pool) *PgPolicyDecisionRepository {
	return &PgPolicyDecisionRepository{pool: pool}
}

// policyOutcomeToString converts a PolicyOutcome to its string representation for storage.
func policyOutcomeToString(o policy.PolicyOutcome) string {
	return o.String()
}

// policyOutcomeFromString converts a string back to a PolicyOutcome.
func policyOutcomeFromString(s string) policy.PolicyOutcome {
	switch s {
	case "allow":
		return policy.OutcomeAllow
	case "allow_with_constraints":
		return policy.OutcomeAllowWithConstraints
	case "require_approval":
		return policy.OutcomeRequireApproval
	case "deny":
		return policy.OutcomeDeny
	default:
		return policy.OutcomeAllow
	}
}

// ruleEvalJSON is used for JSONB serialization of RuleEvaluation.
type ruleEvalJSON struct {
	RuleID  string  `json:"rule_id"`
	Passed  bool    `json:"passed"`
	Outcome *string `json:"outcome,omitempty"`
	Reason  string  `json:"reason"`
}

func (r *PgPolicyDecisionRepository) Save(ctx context.Context, pd *policy.PolicyDecision) error {
	constraintsJSON, err := json.Marshal(pd.Constraints())
	if err != nil {
		return fmt.Errorf("marshaling policy constraints: %w", err)
	}

	var approvalJSON []byte
	if ar := pd.ApprovalRequirement(); ar != nil {
		approvalJSON, err = json.Marshal(ar)
		if err != nil {
			return fmt.Errorf("marshaling approval requirement: %w", err)
		}
	}

	rulesJSON, err := marshalRuleEvaluations(pd.RulesEvaluated())
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO policy_decisions (id, task_id, evaluated_action, outcome, constraints, approval_requirement, rules_evaluated, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		pd.ID().String(),
		pd.TaskID().String(),
		pd.EvaluatedAction(),
		policyOutcomeToString(pd.Outcome()),
		constraintsJSON,
		approvalJSON,
		rulesJSON,
		pd.Reason(),
		pd.CreatedAt().Time,
	)
	if err != nil {
		return fmt.Errorf("inserting policy decision: %w", err)
	}
	return nil
}

func (r *PgPolicyDecisionRepository) FindByTaskID(ctx context.Context, taskID shared.TaskID) (*policy.PolicyDecision, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_id, evaluated_action, outcome, constraints, approval_requirement, rules_evaluated, reason, created_at
		FROM policy_decisions WHERE task_id = $1`, taskID.String())
	return scanPolicyDecision(row)
}

func scanPolicyDecision(row scannable) (*policy.PolicyDecision, error) {
	var (
		id              string
		taskID          string
		evaluatedAction string
		outcome         string
		constraintsJSON []byte
		approvalJSON    []byte
		rulesJSON       []byte
		reason          string
		createdAt       shared.Timestamp
	)

	err := row.Scan(&id, &taskID, &evaluatedAction, &outcome, &constraintsJSON, &approvalJSON, &rulesJSON, &reason, &createdAt.Time)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("policy decision not found")
		}
		return nil, fmt.Errorf("scanning policy decision: %w", err)
	}

	var constraints []policy.PolicyConstraint
	if err := json.Unmarshal(constraintsJSON, &constraints); err != nil {
		return nil, fmt.Errorf("unmarshaling policy constraints: %w", err)
	}

	var approvalReq *policy.ApprovalRequirement
	if len(approvalJSON) > 0 {
		approvalReq = &policy.ApprovalRequirement{}
		if err := json.Unmarshal(approvalJSON, approvalReq); err != nil {
			return nil, fmt.Errorf("unmarshaling approval requirement: %w", err)
		}
	}

	rulesEvaluated, err := unmarshalRuleEvaluations(rulesJSON)
	if err != nil {
		return nil, err
	}

	return policy.Reconstruct(
		shared.PolicyDecisionID(id),
		shared.TaskID(taskID),
		evaluatedAction,
		policyOutcomeFromString(outcome),
		constraints,
		approvalReq,
		rulesEvaluated,
		reason,
		createdAt,
	), nil
}

func marshalRuleEvaluations(evals []policy.RuleEvaluation) ([]byte, error) {
	items := make([]ruleEvalJSON, len(evals))
	for i, e := range evals {
		var outcomeStr *string
		if e.Outcome != nil {
			s := e.Outcome.String()
			outcomeStr = &s
		}
		items[i] = ruleEvalJSON{
			RuleID:  e.RuleID,
			Passed:  e.Passed,
			Outcome: outcomeStr,
			Reason:  e.Reason,
		}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("marshaling rule evaluations: %w", err)
	}
	return data, nil
}

func unmarshalRuleEvaluations(data []byte) ([]policy.RuleEvaluation, error) {
	var items []ruleEvalJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshaling rule evaluations: %w", err)
	}

	evals := make([]policy.RuleEvaluation, len(items))
	for i, item := range items {
		var outcome *policy.PolicyOutcome
		if item.Outcome != nil {
			o := policyOutcomeFromString(*item.Outcome)
			outcome = &o
		}
		evals[i] = policy.RuleEvaluation{
			RuleID:  item.RuleID,
			Passed:  item.Passed,
			Outcome: outcome,
			Reason:  item.Reason,
		}
	}
	return evals, nil
}
