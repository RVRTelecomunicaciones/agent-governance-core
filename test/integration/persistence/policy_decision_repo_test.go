//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: PgPolicyDecisionRepository has no Update method.
var _ interface {
	Save(ctx context.Context, pd *policy.PolicyDecision) error
} = (*persistence.PgPolicyDecisionRepository)(nil)

func TestPolicyDecisionRepo_SaveAndFindByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
	ctx := context.Background()

	pd := fixtures.NewTestPolicyDecision()
	saveTaskForFK(t, db, pd.TaskID())
	require.NoError(t, repo.Save(ctx, pd))

	found, err := repo.FindByTaskID(ctx, pd.TaskID())
	require.NoError(t, err)

	assert.Equal(t, pd.ID(), found.ID())
	assert.Equal(t, pd.TaskID(), found.TaskID())
	assert.Equal(t, pd.EvaluatedAction(), found.EvaluatedAction())
	assert.Equal(t, pd.Outcome(), found.Outcome())
	assert.Equal(t, pd.Reason(), found.Reason())
}

func TestPolicyDecisionRepo_AllOutcomes(t *testing.T) {
	outcomes := []struct {
		name    string
		outcome policy.PolicyOutcome
		opts    []fixtures.PolicyDecisionOption
	}{
		{"allow", policy.OutcomeAllow, nil},
		{"allow_with_constraints", policy.OutcomeAllowWithConstraints, []fixtures.PolicyDecisionOption{
			fixtures.WithPolicyConstraints([]policy.PolicyConstraint{
				{Type: "scope", Value: "read_only", Reason: "test"},
			}),
		}},
		{"require_approval", policy.OutcomeRequireApproval, []fixtures.PolicyDecisionOption{
			fixtures.WithApprovalRequirement(&policy.ApprovalRequirement{
				ApproverRole: "human",
				Reason:       "high risk",
			}),
		}},
		{"deny", policy.OutcomeDeny, nil},
	}

	db := testhelpers.NewTestDB(t)
	ctx := context.Background()

	for _, tc := range outcomes {
		t.Run(tc.name, func(t *testing.T) {
			repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
			opts := append([]fixtures.PolicyDecisionOption{fixtures.WithPolicyOutcome(tc.outcome)}, tc.opts...)
			pd := fixtures.NewTestPolicyDecision(opts...)
			saveTaskForFK(t, db, pd.TaskID())
			require.NoError(t, repo.Save(ctx, pd))

			found, err := repo.FindByTaskID(ctx, pd.TaskID())
			require.NoError(t, err)
			assert.Equal(t, tc.outcome, found.Outcome())
		})
	}
}

func TestPolicyDecisionRepo_ConstraintsJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
	ctx := context.Background()

	constraints := []policy.PolicyConstraint{
		{Type: "scope", Value: "read_only", Reason: "security policy"},
		{Type: "timeout", Value: "60s", Reason: "performance"},
	}

	pd := fixtures.NewTestPolicyDecision(
		fixtures.WithPolicyOutcome(policy.OutcomeAllowWithConstraints),
		fixtures.WithPolicyConstraints(constraints),
	)
	saveTaskForFK(t, db, pd.TaskID())
	require.NoError(t, repo.Save(ctx, pd))

	found, err := repo.FindByTaskID(ctx, pd.TaskID())
	require.NoError(t, err)

	foundConstraints := found.Constraints()
	require.Len(t, foundConstraints, 2)
	assert.Equal(t, "scope", foundConstraints[0].Type)
	assert.Equal(t, "read_only", foundConstraints[0].Value)
	assert.Equal(t, "security policy", foundConstraints[0].Reason)
}

func TestPolicyDecisionRepo_ApprovalRequirementJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
	ctx := context.Background()

	timeout := 600
	ar := &policy.ApprovalRequirement{
		ApproverRole: "admin",
		Reason:       "critical action",
		TimeoutSecs:  &timeout,
	}

	pd := fixtures.NewTestPolicyDecision(
		fixtures.WithPolicyOutcome(policy.OutcomeRequireApproval),
		fixtures.WithApprovalRequirement(ar),
	)
	saveTaskForFK(t, db, pd.TaskID())
	require.NoError(t, repo.Save(ctx, pd))

	found, err := repo.FindByTaskID(ctx, pd.TaskID())
	require.NoError(t, err)

	foundAR := found.ApprovalRequirement()
	require.NotNil(t, foundAR)
	assert.Equal(t, "admin", foundAR.ApproverRole)
	assert.Equal(t, "critical action", foundAR.Reason)
	require.NotNil(t, foundAR.TimeoutSecs)
	assert.Equal(t, 600, *foundAR.TimeoutSecs)
}

func TestPolicyDecisionRepo_NilApprovalRequirement(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
	ctx := context.Background()

	pd := fixtures.NewTestPolicyDecision(fixtures.WithPolicyOutcome(policy.OutcomeAllow))
	saveTaskForFK(t, db, pd.TaskID())
	require.NoError(t, repo.Save(ctx, pd))

	found, err := repo.FindByTaskID(ctx, pd.TaskID())
	require.NoError(t, err)
	assert.Nil(t, found.ApprovalRequirement())
}

func TestPolicyDecisionRepo_RulesEvaluatedJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgPolicyDecisionRepository(db.Pool)
	ctx := context.Background()

	pd := fixtures.NewTestPolicyDecision()
	saveTaskForFK(t, db, pd.TaskID())
	require.NoError(t, repo.Save(ctx, pd))

	found, err := repo.FindByTaskID(ctx, pd.TaskID())
	require.NoError(t, err)

	rules := found.RulesEvaluated()
	require.NotEmpty(t, rules)
	assert.Equal(t, "test-rule-1", rules[0].RuleID)
	assert.True(t, rules[0].Passed)
	require.NotNil(t, rules[0].Outcome)
	assert.Equal(t, policy.OutcomeAllow, *rules[0].Outcome)
	assert.Equal(t, "test rule passed", rules[0].Reason)
}
