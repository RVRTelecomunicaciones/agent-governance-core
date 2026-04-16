package routing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- computeRawAdjustment tests ---

func TestComputeRawAdjustment_AtBaseline_ReturnsZero(t *testing.T) {
	adj := computeRawAdjustment(BaselineFailureRate)
	assert.InDelta(t, 0.0, adj, 1e-9, "at baseline failure rate, adjustment should be zero")
}

func TestComputeRawAdjustment_AboveBaseline_NegativeAdjustment(t *testing.T) {
	adj := computeRawAdjustment(0.50)
	assert.Less(t, adj, 0.0, "above baseline should produce negative (penalty) adjustment")
}

func TestComputeRawAdjustment_BelowBaseline_PositiveAdjustment(t *testing.T) {
	adj := computeRawAdjustment(0.05)
	assert.Greater(t, adj, 0.0, "below baseline should produce positive (bonus) adjustment")
}

func TestComputeRawAdjustment_ZeroFailureRate_MaxBonus(t *testing.T) {
	adj := computeRawAdjustment(0.0)
	assert.InDelta(t, MaxBonus, adj, 1e-9, "zero failure rate should yield MaxBonus")
}

func TestComputeRawAdjustment_FullFailureRate_NegMaxPenalty(t *testing.T) {
	adj := computeRawAdjustment(1.0)
	assert.InDelta(t, -MaxPenalty, adj, 1e-9, "100%% failure rate should yield -MaxPenalty")
}

// --- computeConfidence tests ---

func TestComputeConfidence_AtMinSamples_ReturnsZero(t *testing.T) {
	c := computeConfidence(MinSamples)
	assert.Equal(t, 0.0, c)
}

func TestComputeConfidence_BelowMinSamples_ReturnsZero(t *testing.T) {
	c := computeConfidence(MinSamples - 1)
	assert.Equal(t, 0.0, c)

	c = computeConfidence(0)
	assert.Equal(t, 0.0, c)
}

func TestComputeConfidence_At10Samples_Returns0_2(t *testing.T) {
	// MinSamples=5, FullConfidenceAt=30 → (10-5)/(30-5) = 5/25 = 0.2
	c := computeConfidence(10)
	assert.InDelta(t, 0.2, c, 1e-9)
}

func TestComputeConfidence_AtFullConfidence_ReturnsOne(t *testing.T) {
	c := computeConfidence(FullConfidenceAt)
	assert.Equal(t, 1.0, c)
}

func TestComputeConfidence_AboveFullConfidence_CappedAtOne(t *testing.T) {
	c := computeConfidence(FullConfidenceAt + 100)
	assert.Equal(t, 1.0, c)
}

// --- clampValue tests ---

func TestClampValue_WithinBounds(t *testing.T) {
	assert.Equal(t, 0.5, clampValue(0.5, 0.0, 1.0))
}

func TestClampValue_BelowMin(t *testing.T) {
	assert.Equal(t, 0.0, clampValue(-0.5, 0.0, 1.0))
}

func TestClampValue_AboveMax(t *testing.T) {
	assert.Equal(t, 1.0, clampValue(1.5, 0.0, 1.0))
}

// --- computeAdjustment degradation tests ---

func TestComputeAdjustment_NilStats_ReturnsNil(t *testing.T) {
	adj := computeAdjustment(StrategyDirect, nil)
	assert.Nil(t, adj)
}

func TestComputeAdjustment_Level1_UsesStrategyRoleCategory(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: string(RoleImplementer), Category: "tool"}: {
				Total: 20, Failures: 10, Rate: 0.5,
			},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{},
		ByStrategy:         map[RoutingStrategy]FailureRate{},
		ComputedAt:         time.Now(),
		Window:             time.Hour,
	}

	adj := computeAdjustment(StrategyDirect, stats)
	require.NotNil(t, adj)
	assert.Equal(t, "strategy_role_category", adj.Granularity)
	assert.Equal(t, 20, adj.SampleSize)
	assert.InDelta(t, 0.5, adj.FailureRate, 1e-9)
}

func TestComputeAdjustment_Level1Insufficient_DegradesToLevel2(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			// Only 3 samples — below MinSamples
			{Strategy: StrategyDirect, Role: string(RoleImplementer), Category: "tool"}: {
				Total: 3, Failures: 1, Rate: 0.33,
			},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Category: "tool"}: {
				Total: 15, Failures: 3, Rate: 0.2,
			},
		},
		ByStrategy: map[RoutingStrategy]FailureRate{},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	adj := computeAdjustment(StrategyDirect, stats)
	require.NotNil(t, adj)
	assert.Equal(t, "strategy_category", adj.Granularity)
	assert.Equal(t, 15, adj.SampleSize)
}

func TestComputeAdjustment_Level1And2Insufficient_DegradesToLevel3(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: string(RoleImplementer), Category: "tool"}: {
				Total: 2, Failures: 1, Rate: 0.5,
			},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Category: "tool"}: {
				Total: 3, Failures: 1, Rate: 0.33,
			},
		},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 50, Failures: 10, Rate: 0.2},
		},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	adj := computeAdjustment(StrategyDirect, stats)
	require.NotNil(t, adj)
	assert.Equal(t, "strategy", adj.Granularity)
	assert.Equal(t, 50, adj.SampleSize)
}

func TestComputeAdjustment_AllInsufficient_ReturnsNil(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: string(RoleImplementer), Category: "tool"}: {
				Total: 2, Failures: 1, Rate: 0.5,
			},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Category: "tool"}: {
				Total: 3, Failures: 1, Rate: 0.33,
			},
		},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 4, Failures: 1, Rate: 0.25},
		},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.Nil(t, adj, "all levels insufficient — should return nil")
}

func TestComputeAdjustment_FinalAdjustment_IsRawTimesConfidence(t *testing.T) {
	stats := &FailureStats{
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 10, Failures: 5, Rate: 0.5},
		},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	adj := computeAdjustment(StrategyDirect, stats)
	require.NotNil(t, adj)

	expectedRaw := computeRawAdjustment(0.5)
	expectedConf := computeConfidence(10)
	assert.InDelta(t, expectedRaw, adj.RawAdjustment, 1e-9)
	assert.InDelta(t, expectedConf, adj.Confidence, 1e-9)
	assert.InDelta(t, expectedRaw*expectedConf, adj.FinalAdjustment, 1e-9)
}

// --- applyAdaptiveAdjustments tests ---

func TestApplyAdaptiveAdjustments_NilStats_AdjustedScoreEqualsScore(t *testing.T) {
	evals := []StrategyEvaluation{
		{Strategy: StrategyDirect, Score: 0.80},
		{Strategy: StrategyDecompose, Score: 0.65},
		{Strategy: StrategyEscalate, Score: 0.40},
	}

	result := applyAdaptiveAdjustments(evals, nil)
	require.Len(t, result, 3)
	for i, r := range result {
		assert.Equal(t, evals[i].Score, r.AdjustedScore,
			"with nil stats, adjusted score should equal original score for %s", r.Strategy)
		assert.Nil(t, r.AdaptiveAdjustment)
	}
}

func TestApplyAdaptiveAdjustments_WithStats_AppliesAdjustment(t *testing.T) {
	stats := &FailureStats{
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 30, Failures: 15, Rate: 0.5}, // above baseline → penalty
		},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	evals := []StrategyEvaluation{
		{Strategy: StrategyDirect, Score: 0.80},
	}

	result := applyAdaptiveAdjustments(evals, stats)
	require.Len(t, result, 1)
	assert.Less(t, result[0].AdjustedScore, result[0].Score,
		"high failure rate should reduce adjusted score")
	require.NotNil(t, result[0].AdaptiveAdjustment)
	assert.Equal(t, "strategy", result[0].AdaptiveAdjustment.Granularity)
}

func TestApplyAdaptiveAdjustments_NoDataForStrategy_AdjustedEqualsScore(t *testing.T) {
	stats := &FailureStats{
		ByStrategy: map[RoutingStrategy]FailureRate{
			// Only data for escalate, not direct
			StrategyEscalate: {Total: 30, Failures: 15, Rate: 0.5},
		},
		ComputedAt: time.Now(),
		Window:     time.Hour,
	}

	evals := []StrategyEvaluation{
		{Strategy: StrategyDirect, Score: 0.80},
	}

	result := applyAdaptiveAdjustments(evals, stats)
	require.Len(t, result, 1)
	assert.Equal(t, 0.80, result[0].AdjustedScore)
	assert.Nil(t, result[0].AdaptiveAdjustment)
}

// --- ExtractCategory tests ---

func TestExtractCategory_WithSlash(t *testing.T) {
	assert.Equal(t, "tool", ExtractCategory("tool/shell_timeout"))
}

func TestExtractCategory_RuntimePrefix(t *testing.T) {
	assert.Equal(t, "runtime", ExtractCategory("runtime/oom"))
}

func TestExtractCategory_NoSlash(t *testing.T) {
	assert.Equal(t, "noprefix", ExtractCategory("noprefix"))
}

func TestExtractCategory_EmptyString(t *testing.T) {
	assert.Equal(t, "", ExtractCategory(""))
}

func TestExtractCategory_MultipleSlashes(t *testing.T) {
	assert.Equal(t, "tool", ExtractCategory("tool/shell/timeout"))
}

// --- JSON roundtrip tests ---

func TestStrategyEvaluation_JSONRoundtrip_WithAdaptive(t *testing.T) {
	eval := StrategyEvaluation{
		Strategy:      StrategyDirect,
		Score:         0.72,
		AdjustedScore: 0.6731,
		FactorScores:  map[string]float64{"risk_level": 0.9},
		Overridden:    false,
		Reason:        "test",
		AdaptiveAdjustment: &AdaptiveAdjustment{
			RawAdjustment:   -0.046875,
			Confidence:      1.0,
			FinalAdjustment: -0.046875,
			FailureRate:     0.45,
			SampleSize:      40,
			Granularity:     "strategy×role×category",
			Window:          "48h",
		},
	}

	data, err := json.Marshal(eval)
	require.NoError(t, err)

	var decoded StrategyEvaluation
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, eval.Score, decoded.Score)
	assert.Equal(t, eval.AdjustedScore, decoded.AdjustedScore)
	require.NotNil(t, decoded.AdaptiveAdjustment)
	assert.Equal(t, eval.AdaptiveAdjustment.Granularity, decoded.AdaptiveAdjustment.Granularity)
	assert.Equal(t, eval.AdaptiveAdjustment.SampleSize, decoded.AdaptiveAdjustment.SampleSize)
	assert.InDelta(t, eval.AdaptiveAdjustment.FinalAdjustment, decoded.AdaptiveAdjustment.FinalAdjustment, 0.0001)
}

func TestStrategyEvaluation_JSONRoundtrip_WithoutAdaptive(t *testing.T) {
	eval := StrategyEvaluation{
		Strategy:      StrategyDirect,
		Score:         0.72,
		AdjustedScore: 0.72,
		FactorScores:  map[string]float64{"risk_level": 0.9},
	}

	data, err := json.Marshal(eval)
	require.NoError(t, err)

	// AdaptiveAdjustment should be omitted from JSON (omitempty)
	assert.NotContains(t, string(data), "adaptive_adjustment")

	var decoded StrategyEvaluation
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Nil(t, decoded.AdaptiveAdjustment)
	assert.Equal(t, eval.AdjustedScore, decoded.AdjustedScore)
}
