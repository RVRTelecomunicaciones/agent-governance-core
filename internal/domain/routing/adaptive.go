package routing

import (
	"strings"
	"time"
)

// Adaptive routing constants.
const (
	MaxPenalty          = 0.15
	MaxBonus            = 0.05
	BaselineFailureRate = 0.20
	MinSamples          = 5
	FullConfidenceAt    = 30
)

// AdaptiveAdjustment is a value object describing the adaptive score adjustment
// applied to a single strategy evaluation.
type AdaptiveAdjustment struct {
	RawAdjustment   float64 `json:"raw_adjustment"`
	Confidence      float64 `json:"confidence"`
	FinalAdjustment float64 `json:"final_adjustment"`
	FailureRate     float64 `json:"failure_rate"`
	SampleSize      int     `json:"sample_size"`
	Granularity     string  `json:"granularity"`
	Window          string  `json:"window"`
}

// computeAdjustment applies a 4-level degradation cascade to find the best
// available failure data for the given strategy. Returns nil when no data
// with sufficient samples exists.
func computeAdjustment(strategy RoutingStrategy, stats *FailureStats) *AdaptiveAdjustment {
	if stats == nil {
		return nil
	}

	role := string(defaultRoleMapping[strategy])

	// Level 1: strategy + role + category (aggregated across categories for that strategy+role)
	if stats.ByStrategyRoleCategory != nil {
		agg := aggregateByStrategyAndRole(stats.ByStrategyRoleCategory, strategy, role)
		if agg.Total >= MinSamples {
			return buildAdjustment(agg, "strategy_role_category", stats.Window)
		}
	}

	// Level 2: strategy + category (aggregated across categories for that strategy)
	if stats.ByStrategyCategory != nil {
		agg := aggregateByStrategyFromMap(stats.ByStrategyCategory, strategy)
		if agg.Total >= MinSamples {
			return buildAdjustment(agg, "strategy_category", stats.Window)
		}
	}

	// Level 3: strategy only
	if stats.ByStrategy != nil {
		if fr, ok := stats.ByStrategy[strategy]; ok && fr.Total >= MinSamples {
			return buildAdjustment(fr, "strategy", stats.Window)
		}
	}

	// Level 4: no sufficient data
	return nil
}

// aggregateByStrategyAndRole sums FailureRate entries from m whose StatsKey
// matches the given strategy and role.
func aggregateByStrategyAndRole(m map[StatsKey]FailureRate, strategy RoutingStrategy, role string) FailureRate {
	var agg FailureRate
	for k, v := range m {
		if k.Strategy == strategy && k.Role == role {
			agg.Total += v.Total
			agg.Failures += v.Failures
		}
	}
	if agg.Total > 0 {
		agg.Rate = float64(agg.Failures) / float64(agg.Total)
	}
	return agg
}

// aggregateByStrategyFromMap sums FailureRate entries from m whose StatsKey
// matches the given strategy (ignoring role).
func aggregateByStrategyFromMap(m map[StatsKey]FailureRate, strategy RoutingStrategy) FailureRate {
	var agg FailureRate
	for k, v := range m {
		if k.Strategy == strategy {
			agg.Total += v.Total
			agg.Failures += v.Failures
		}
	}
	if agg.Total > 0 {
		agg.Rate = float64(agg.Failures) / float64(agg.Total)
	}
	return agg
}

// buildAdjustment creates an AdaptiveAdjustment from a FailureRate and metadata.
func buildAdjustment(fr FailureRate, granularity string, window time.Duration) *AdaptiveAdjustment {
	raw := computeRawAdjustment(fr.Rate)
	conf := computeConfidence(fr.Total)
	final := raw * conf

	return &AdaptiveAdjustment{
		RawAdjustment:   raw,
		Confidence:      conf,
		FinalAdjustment: final,
		FailureRate:     fr.Rate,
		SampleSize:      fr.Total,
		Granularity:     granularity,
		Window:          window.String(),
	}
}

// computeRawAdjustment computes a proportional adjustment relative to the
// baseline failure rate. Below baseline → positive (bonus), above → negative (penalty).
// Clamped to [-MaxPenalty, +MaxBonus].
func computeRawAdjustment(failureRate float64) float64 {
	// deviation: positive when failure rate is below baseline (good), negative when above (bad)
	deviation := BaselineFailureRate - failureRate

	// Scale so that full range maps linearly:
	//   failureRate=0.0 → deviation=0.20 → MaxBonus
	//   failureRate=BaselineFailureRate → deviation=0.0 → 0
	//   failureRate=1.0 → deviation=-0.80 → -MaxPenalty
	var raw float64
	if deviation >= 0 {
		// Below baseline: scale to [0, MaxBonus]
		raw = (deviation / BaselineFailureRate) * MaxBonus
	} else {
		// Above baseline: scale to [-MaxPenalty, 0]
		raw = (deviation / (1.0 - BaselineFailureRate)) * MaxPenalty
	}

	return clampValue(raw, -MaxPenalty, MaxBonus)
}

// computeConfidence returns a linear ramp from 0.0 at MinSamples to 1.0 at
// FullConfidenceAt. Below MinSamples returns 0.0, above FullConfidenceAt returns 1.0.
func computeConfidence(samples int) float64 {
	if samples <= MinSamples {
		return 0.0
	}
	if samples >= FullConfidenceAt {
		return 1.0
	}
	return float64(samples-MinSamples) / float64(FullConfidenceAt-MinSamples)
}

// clampValue clamps value to [min, max].
func clampValue(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// applyAdaptiveAdjustments enriches each evaluation with adaptive scoring data.
// When stats is nil or no adjustment is available, AdjustedScore equals Score.
func applyAdaptiveAdjustments(evals []StrategyEvaluation, stats *FailureStats) []StrategyEvaluation {
	result := make([]StrategyEvaluation, len(evals))
	for i, eval := range evals {
		result[i] = eval
		result[i].AdjustedScore = eval.Score

		if stats == nil {
			continue
		}

		adj := computeAdjustment(eval.Strategy, stats)
		if adj == nil {
			continue
		}

		result[i].AdaptiveAdjustment = adj
		adjusted := eval.Score + adj.FinalAdjustment
		result[i].AdjustedScore = clampValue(adjusted, eval.Score-MaxPenalty, eval.Score+MaxBonus)
	}
	return result
}

// ExtractCategory extracts the category prefix from a failure code.
// For example, "tool/shell_timeout" returns "tool", "runtime/oom" returns "runtime".
// If there is no "/" separator, the entire code is returned as the category.
func ExtractCategory(code string) string {
	if idx := strings.Index(code, "/"); idx >= 0 {
		return code[:idx]
	}
	return code
}
