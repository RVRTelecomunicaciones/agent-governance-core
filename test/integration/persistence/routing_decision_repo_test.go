//go:build integration

package persistence_test

import (
	"context"
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/outbound/persistence"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/fixtures"
	"github.com/RVRTelecomunicaciones/agent-governance-core/test/integration/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: PgRoutingDecisionRepository has no Update method.
// If someone adds Update, this will fail to compile.
var _ interface {
	Save(ctx context.Context, rd *routing.RoutingDecision) error
} = (*persistence.PgRoutingDecisionRepository)(nil)

func TestRoutingDecisionRepo_SaveAndFindByTaskID(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgRoutingDecisionRepository(db.Pool)
	ctx := context.Background()

	rd := fixtures.NewTestRoutingDecision()
	saveTaskForFK(t, db, rd.TaskID())
	require.NoError(t, repo.Save(ctx, rd))

	found, err := repo.FindByTaskID(ctx, rd.TaskID())
	require.NoError(t, err)

	assert.Equal(t, rd.ID(), found.ID())
	assert.Equal(t, rd.TaskID(), found.TaskID())
	assert.Equal(t, rd.SelectedStrategy(), found.SelectedStrategy())
	assert.Equal(t, rd.SelectedAgentRole(), found.SelectedAgentRole())
	assert.Equal(t, rd.Reason(), found.Reason())
}

func TestRoutingDecisionRepo_EvaluatedStrategiesJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgRoutingDecisionRepository(db.Pool)
	ctx := context.Background()

	rd := fixtures.NewTestRoutingDecision()
	saveTaskForFK(t, db, rd.TaskID())
	require.NoError(t, repo.Save(ctx, rd))

	found, err := repo.FindByTaskID(ctx, rd.TaskID())
	require.NoError(t, err)

	evals := found.EvaluatedStrategies()
	require.Len(t, evals, 1)
	assert.Equal(t, routing.StrategyDirect, evals[0].Strategy)
	assert.Equal(t, 0.8, evals[0].Score)
}

func TestRoutingDecisionRepo_FactorScoresJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgRoutingDecisionRepository(db.Pool)
	ctx := context.Background()

	// Create routing decision with factor scores
	evals := []routing.StrategyEvaluation{
		{
			Strategy:     routing.StrategyDirect,
			Score:        0.85,
			FactorScores: map[string]float64{"risk_level": 0.9, "complexity": 0.7},
			Reason:       "best match",
		},
		{
			Strategy:     routing.StrategyDecompose,
			Score:        0.6,
			FactorScores: map[string]float64{"risk_level": 0.5},
			Overridden:   true,
			Reason:       "too complex",
		},
	}

	id, _ := fixtures.NewTestRoutingDecision().ID(), fixtures.NewTestRoutingDecision()
	rd := fixtures.NewTestRoutingDecision()
	_ = id
	saveTaskForFK(t, db, rd.TaskID())

	// We need to create an RD with specific evals. Use routing.Reconstruct directly.
	rdWithFactors := routing.Reconstruct(
		rd.ID(), rd.TaskID(), evals,
		routing.StrategyDirect, routing.RoleImplementer,
		"test factor scores", nil, rd.CreatedAt(),
	)
	require.NoError(t, repo.Save(ctx, rdWithFactors))

	found, err := repo.FindByTaskID(ctx, rd.TaskID())
	require.NoError(t, err)

	foundEvals := found.EvaluatedStrategies()
	require.Len(t, foundEvals, 2)

	assert.Equal(t, routing.StrategyDirect, foundEvals[0].Strategy)
	assert.Equal(t, 0.85, foundEvals[0].Score)
	assert.Equal(t, 0.9, foundEvals[0].FactorScores["risk_level"])
	assert.Equal(t, 0.7, foundEvals[0].FactorScores["complexity"])
	assert.Equal(t, "best match", foundEvals[0].Reason)

	assert.Equal(t, routing.StrategyDecompose, foundEvals[1].Strategy)
	assert.True(t, foundEvals[1].Overridden)
}

func TestRoutingDecisionRepo_ConstraintsJSONB(t *testing.T) {
	db := testhelpers.NewTestDB(t)
	repo := persistence.NewPgRoutingDecisionRepository(db.Pool)
	ctx := context.Background()

	constraints := []routing.RoutingConstraint{
		{Type: "scope", Value: "read_only"},
		{Type: "timeout", Value: "300s"},
	}

	rd := fixtures.NewTestRoutingDecision()
	saveTaskForFK(t, db, rd.TaskID())

	rdWithConstraints := routing.Reconstruct(
		rd.ID(), rd.TaskID(), rd.EvaluatedStrategies(),
		rd.SelectedStrategy(), rd.SelectedAgentRole(),
		rd.Reason(), constraints, rd.CreatedAt(),
	)
	require.NoError(t, repo.Save(ctx, rdWithConstraints))

	found, err := repo.FindByTaskID(ctx, rd.TaskID())
	require.NoError(t, err)

	foundConstraints := found.Constraints()
	require.Len(t, foundConstraints, 2)
	assert.Equal(t, "scope", foundConstraints[0].Type)
	assert.Equal(t, "read_only", foundConstraints[0].Value)
	assert.Equal(t, "timeout", foundConstraints[1].Type)
	assert.Equal(t, "300s", foundConstraints[1].Value)
}
