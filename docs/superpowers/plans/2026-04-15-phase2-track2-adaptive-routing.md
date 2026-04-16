# Phase 2 Track 2: Adaptive Routing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add failure-history-driven score adjustments to the routing evaluator — automatic, bounded, explainable, with graceful degradation.

**Architecture:** New Phase 2.5 in routing evaluator (between scoring and tiebreaker). FailureStats in-memory aggregate refreshed by background goroutine every 5min from audit_entries (48h window). Bounded additive adjustment with 4-level degradation by granularity. ADAPTIVE_ROUTING_ENABLED flag for backwards compatibility.

**Tech Stack:** Go 1.26.2, existing routing evaluator, `sync/atomic` for lock-free reads, audit_entries as data source

**Spec:** `docs/superpowers/specs/2026-04-15-phase2-track2-adaptive-routing-design.md`

**Baseline invariant:** All existing tests must pass. `ADAPTIVE_ROUTING_ENABLED=false` must produce identical behavior to v0.2.0.

---

## Block Execution Map

| Task | Block | Parallel with | Done criteria |
|---|---|---|---|
| T1: Adaptive domain types + formula | B1 | None (foundation) | All adaptive types compile, formula tests pass |
| T2: Evaluator Phase 2.5 | B2 | T3 | Evaluator uses AdjustedScore, adaptive tests pass, existing tests pass |
| T3: FailureStatsCollector + Store | B3 | T2 | Collector compiles, store tests pass, refresh logic verified |
| T4: Config + wiring + route_task | B4 | None | Full build, all tests pass, conditional wiring works |
| T5: Tracing + persistence update | B5 | None | Adaptive trace attrs emitted, JSONB roundtrip works |
| T6: Integration test | B6 | None | End-to-end adaptive routing verified with real PG |

### Dependency Graph

```
T1 (adaptive domain)
 ├── T2 (evaluator Phase 2.5) ─── parallel
 └── T3 (collector + store) ────── parallel
         │
         T4 (config + wiring)
         │
         T5 (tracing + persistence)
         │
         T6 (integration test)
```

---

## File Structure

```
internal/
  domain/
    routing/
      adaptive.go                        — NEW: AdaptiveAdjustment VO, constants, formula functions
      adaptive_test.go                   — NEW: formula, confidence, degradation, clamp tests
      failure_stats.go                   — NEW: FailureStats, FailureRate, StatsKey types
      strategy.go                        — MODIFY: add AdjustedScore + AdaptiveAdjustment to StrategyEvaluation
      evaluator.go                       — MODIFY: add Phase 2.5, tiebreaker uses AdjustedScore
      evaluator_test.go                  — MODIFY: add adaptive evaluator tests
  application/
    routing/
      failure_stats_store.go             — NEW: FailureStatsStore (atomic pointer)
      failure_stats_collector.go         — NEW: FailureStatsCollector (background goroutine)
      failure_stats_collector_test.go    — NEW: collector + store tests
      route_task.go                      — MODIFY: pass FailureStats to Evaluate()
  adapters/
    inbound/
      tracing/
        governance_traced.go             — MODIFY: add adaptive trace attributes to RouteTask span
    outbound/
      persistence/
        pg_routing_decision_repo.go      — MODIFY: JSONB now includes AdjustedScore + AdaptiveAdjustment (no schema change)
  infrastructure/
    config/
      config.go                          — MODIFY: add AdaptiveRoutingEnabled
  bootstrap/
    wire.go                              — MODIFY: conditional collector start + FailureStats injection
test/
  integration/
    adaptive/
      adaptive_routing_test.go           — NEW: end-to-end adaptive routing with real PG
```

---

## Task 1: Adaptive Domain Types + Formula (B1)

**Files:**
- Create: `internal/domain/routing/failure_stats.go`
- Create: `internal/domain/routing/adaptive.go`
- Create: `internal/domain/routing/adaptive_test.go`
- Modify: `internal/domain/routing/strategy.go`

- [ ] **Step 1: Create FailureStats types**

```go
// internal/domain/routing/failure_stats.go
package routing

import "time"

// StatsKey identifies a failure rate aggregation bucket.
type StatsKey struct {
	Strategy RoutingStrategy
	Role     string // AgentRole as string; empty at coarser levels
	Category string // failure_category; empty at coarser levels
}

// FailureRate holds the failure rate for a specific aggregation bucket.
type FailureRate struct {
	Total    int     // total attempts
	Failures int     // failed attempts
	Rate     float64 // Failures / Total (0.0 - 1.0)
}

// FailureStats holds the in-memory failure rate snapshot across all aggregation levels.
type FailureStats struct {
	// Fine-grained: strategy × role × category
	ByStrategyRoleCategory map[StatsKey]FailureRate
	// Medium: strategy × category
	ByStrategyCategory map[StatsKey]FailureRate
	// Coarse: strategy only
	ByStrategy map[RoutingStrategy]FailureRate
	// Metadata
	ComputedAt time.Time
	Window     time.Duration
}
```

- [ ] **Step 2: Write failing tests for adaptive formula**

```go
// internal/domain/routing/adaptive_test.go
package routing

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeRawAdjustment_AtBaseline(t *testing.T) {
	adj := computeRawAdjustment(BaselineFailureRate) // 0.20
	assert.Equal(t, 0.0, adj)
}

func TestComputeRawAdjustment_AboveBaseline(t *testing.T) {
	adj := computeRawAdjustment(0.45)
	// excess = 0.25, normalized = 0.25/0.80 = 0.3125, raw = -0.3125 * 0.15 = -0.046875
	assert.InDelta(t, -0.046875, adj, 0.0001)
	assert.True(t, adj >= -MaxPenalty)
}

func TestComputeRawAdjustment_BelowBaseline(t *testing.T) {
	adj := computeRawAdjustment(0.05)
	// deficit = 0.15, normalized = 0.15/0.20 = 0.75, raw = 0.75 * 0.05 = 0.0375
	assert.InDelta(t, 0.0375, adj, 0.0001)
	assert.True(t, adj <= MaxBonus)
}

func TestComputeRawAdjustment_FullFailure(t *testing.T) {
	adj := computeRawAdjustment(1.0)
	assert.InDelta(t, -MaxPenalty, adj, 0.0001)
}

func TestComputeRawAdjustment_ZeroFailure(t *testing.T) {
	adj := computeRawAdjustment(0.0)
	assert.InDelta(t, MaxBonus, adj, 0.0001)
}

func TestComputeConfidence_AtMinSamples(t *testing.T) {
	conf := computeConfidence(MinSamples) // 5
	assert.Equal(t, 0.0, conf)
}

func TestComputeConfidence_BelowMinSamples(t *testing.T) {
	conf := computeConfidence(3)
	assert.Equal(t, 0.0, conf)
}

func TestComputeConfidence_AboveMin(t *testing.T) {
	conf := computeConfidence(10)
	// (10-5)/(30-5) = 5/25 = 0.2
	assert.InDelta(t, 0.2, conf, 0.0001)
}

func TestComputeConfidence_AtFull(t *testing.T) {
	conf := computeConfidence(FullConfidenceAt) // 30
	assert.Equal(t, 1.0, conf)
}

func TestComputeConfidence_AboveFull(t *testing.T) {
	conf := computeConfidence(100)
	assert.Equal(t, 1.0, conf) // capped
}

func TestClamp(t *testing.T) {
	assert.Equal(t, 0.5, clampValue(0.5, 0.3, 0.7))
	assert.Equal(t, 0.3, clampValue(0.1, 0.3, 0.7))
	assert.Equal(t, 0.7, clampValue(0.9, 0.3, 0.7))
}

func TestComputeAdjustment_Level1(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: "implementer", Category: "tool"}: {Total: 40, Failures: 18, Rate: 0.45},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{},
		ByStrategy:         map[RoutingStrategy]FailureRate{},
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.NotNil(t, adj)
	assert.Equal(t, "strategy×role×category", adj.Granularity)
	assert.Equal(t, 40, adj.SampleSize)
	assert.InDelta(t, 0.45, adj.FailureRate, 0.001)
	assert.True(t, adj.FinalAdjustment < 0) // penalty
}

func TestComputeAdjustment_DegradesToLevel2(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: "implementer", Category: "tool"}: {Total: 2, Failures: 1, Rate: 0.5}, // insufficient
		},
		ByStrategyCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Category: "tool"}: {Total: 20, Failures: 8, Rate: 0.4},
		},
		ByStrategy: map[RoutingStrategy]FailureRate{},
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.NotNil(t, adj)
	assert.Equal(t, "strategy×category", adj.Granularity)
	assert.Equal(t, 20, adj.SampleSize)
}

func TestComputeAdjustment_DegradesToLevel3(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{},
		ByStrategyCategory:     map[StatsKey]FailureRate{},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 15, Failures: 3, Rate: 0.2},
		},
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.NotNil(t, adj)
	assert.Equal(t, "strategy", adj.Granularity)
}

func TestComputeAdjustment_NilWhenNoData(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{},
		ByStrategyCategory:     map[StatsKey]FailureRate{},
		ByStrategy:             map[RoutingStrategy]FailureRate{},
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.Nil(t, adj)
}

func TestComputeAdjustment_NilStats(t *testing.T) {
	adj := computeAdjustment(StrategyDirect, nil)
	assert.Nil(t, adj)
}

func TestFinalAdjustment_RawTimesConfidence(t *testing.T) {
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{},
		ByStrategyCategory:     map[StatsKey]FailureRate{},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect: {Total: 10, Failures: 5, Rate: 0.5},
		},
	}

	adj := computeAdjustment(StrategyDirect, stats)
	assert.NotNil(t, adj)
	// confidence at 10 samples = (10-5)/25 = 0.2
	// raw for 0.5 rate = -(0.3/0.8)*0.15 = -0.05625
	// final = -0.05625 * 0.2 = -0.01125
	assert.InDelta(t, adj.RawAdjustment*adj.Confidence, adj.FinalAdjustment, 0.0001)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/domain/routing/... -v -run TestCompute -count=1`
Expected: FAIL — functions not defined

- [ ] **Step 4: Implement adaptive formula**

```go
// internal/domain/routing/adaptive.go
package routing

import "math"

// Adaptive routing constants.
const (
	MaxPenalty          = 0.15
	MaxBonus            = 0.05
	BaselineFailureRate = 0.20
	MinSamples          = 5
	FullConfidenceAt    = 30
)

// AdaptiveAdjustment records how a strategy's score was adjusted by failure history.
// nil means no adaptation was applied.
type AdaptiveAdjustment struct {
	RawAdjustment   float64 `json:"raw_adjustment"`
	Confidence      float64 `json:"confidence"`
	FinalAdjustment float64 `json:"final_adjustment"`
	FailureRate     float64 `json:"failure_rate"`
	SampleSize      int     `json:"sample_size"`
	Granularity     string  `json:"granularity"` // "strategy×role×category", "strategy×category", "strategy"
	Window          string  `json:"window"`      // "48h"
}

// computeAdjustment finds the best available failure data for a strategy and computes the adjustment.
// Returns nil if no data meets MinSamples at any granularity level, or if stats is nil.
func computeAdjustment(strategy RoutingStrategy, stats *FailureStats) *AdaptiveAdjustment {
	if stats == nil {
		return nil
	}

	role := string(defaultRoleMapping[strategy])

	var rate float64
	var samples int
	var granularity string

	// Level 1: strategy × role — aggregate across all categories for this strategy+role
	if fr := aggregateByStrategyAndRole(stats.ByStrategyRoleCategory, strategy, role); fr.Total >= MinSamples {
		rate = fr.Rate
		samples = fr.Total
		granularity = "strategy×role×category"
	// Level 2: strategy — aggregate across all categories (any role)
	} else if fr := aggregateByStrategyFromMap(stats.ByStrategyCategory, strategy); fr.Total >= MinSamples {
		rate = fr.Rate
		samples = fr.Total
		granularity = "strategy×category"
	// Level 3: strategy only
	} else if fr, ok := stats.ByStrategy[strategy]; ok && fr.Total >= MinSamples {
		rate = fr.Rate
		samples = fr.Total
		granularity = "strategy"
	} else {
		return nil // Level 4: no adjustment
	}

	raw := computeRawAdjustment(rate)
	confidence := computeConfidence(samples)
	final := raw * confidence

	return &AdaptiveAdjustment{
		RawAdjustment:   raw,
		Confidence:      confidence,
		FinalAdjustment: final,
		FailureRate:     rate,
		SampleSize:      samples,
		Granularity:     granularity,
		Window:          "48h",
	}
}

// aggregateByStrategyAndRole sums all FailureRate entries matching strategy+role across all categories.
func aggregateByStrategyAndRole(m map[StatsKey]FailureRate, strategy RoutingStrategy, role string) FailureRate {
	var total, failures int
	for k, v := range m {
		if k.Strategy == strategy && k.Role == role {
			total += v.Total
			failures += v.Failures
		}
	}
	if total == 0 {
		return FailureRate{}
	}
	return FailureRate{Total: total, Failures: failures, Rate: float64(failures) / float64(total)}
}

// aggregateByStrategyFromMap sums all FailureRate entries matching strategy across all roles and categories.
func aggregateByStrategyFromMap(m map[StatsKey]FailureRate, strategy RoutingStrategy) FailureRate {
	var total, failures int
	for k, v := range m {
		if k.Strategy == strategy {
			total += v.Total
			failures += v.Failures
		}
	}
	if total == 0 {
		return FailureRate{}
	}
	return FailureRate{Total: total, Failures: failures, Rate: float64(failures) / float64(total)}
}

// computeRawAdjustment calculates the raw score adjustment from a failure rate.
// Above baseline → negative (penalty). Below baseline → positive (bonus). At baseline → 0.
func computeRawAdjustment(failureRate float64) float64 {
	if failureRate > BaselineFailureRate {
		excess := failureRate - BaselineFailureRate
		normalized := math.Min(excess/(1.0-BaselineFailureRate), 1.0)
		return -normalized * MaxPenalty
	}
	if failureRate < BaselineFailureRate {
		deficit := BaselineFailureRate - failureRate
		normalized := math.Min(deficit/BaselineFailureRate, 1.0)
		return normalized * MaxBonus
	}
	return 0
}

// computeConfidence returns a linear confidence factor based on sample size.
// At MinSamples → 0.0. At FullConfidenceAt → 1.0. Below MinSamples → 0.0.
func computeConfidence(samples int) float64 {
	if samples <= MinSamples {
		return 0.0
	}
	conf := float64(samples-MinSamples) / float64(FullConfidenceAt-MinSamples)
	return math.Min(conf, 1.0)
}

// clampValue constrains value to [min, max].
func clampValue(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// applyAdaptiveAdjustments applies failure-history-based adjustments to scored evaluations.
// When stats is nil, all AdjustedScores equal their base Scores (no adaptation).
func applyAdaptiveAdjustments(evals []StrategyEvaluation, stats *FailureStats) []StrategyEvaluation {
	for i := range evals {
		evals[i].AdjustedScore = evals[i].Score // default: no adjustment

		if stats == nil {
			continue
		}

		adj := computeAdjustment(evals[i].Strategy, stats)
		if adj != nil {
			evals[i].AdjustedScore = clampValue(
				evals[i].Score+adj.FinalAdjustment,
				evals[i].Score-MaxPenalty,
				evals[i].Score+MaxBonus,
			)
			evals[i].AdaptiveAdjustment = adj
		}
	}
	return evals
}
```

- [ ] **Step 5: Add AdjustedScore + AdaptiveAdjustment to StrategyEvaluation**

Modify `internal/domain/routing/strategy.go` — add two fields to `StrategyEvaluation`:

```go
type StrategyEvaluation struct {
	Strategy           RoutingStrategy    `json:"strategy"`
	Score              float64            `json:"score"`
	AdjustedScore      float64            `json:"adjusted_score"`
	FactorScores       map[string]float64 `json:"factor_scores"`
	Overridden         bool               `json:"overridden"`
	Reason             string             `json:"reason"`
	AdaptiveAdjustment *AdaptiveAdjustment `json:"adaptive_adjustment,omitempty"`
}
```

Add JSON tags to all fields for JSONB serialization. The `AdaptiveAdjustment` uses `omitempty` so it's absent when nil.

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/domain/routing/... -v -count=1`
Expected: ALL PASS (new adaptive tests + existing evaluator tests)

- [ ] **Step 7: Commit**

```bash
git add internal/domain/routing/failure_stats.go internal/domain/routing/adaptive.go internal/domain/routing/adaptive_test.go internal/domain/routing/strategy.go
git commit -m "feat(adaptive): add adaptive routing types, formula, and degradation logic"
```

---

## Task 2: Evaluator Phase 2.5 (B2)

**Files:**
- Modify: `internal/domain/routing/evaluator.go`
- Modify: `internal/domain/routing/evaluator_test.go`

- [ ] **Step 1: Add FailureStats to EvaluatorInput**

Modify `internal/domain/routing/evaluator.go`:

```go
type EvaluatorInput struct {
	Task          *task.Task
	MemoryContext *MemoryContext
	FailureStats  *FailureStats // nil means no adaptation
}
```

- [ ] **Step 2: Insert Phase 2.5 and modify tiebreaker**

Modify the `Evaluate` function:

```go
func Evaluate(input EvaluatorInput) EvaluatorResult {
	t := input.Task

	// Phase 1: Hard overrides (UNCHANGED)
	if result, ok := checkOverrides(t); ok {
		return result
	}

	// Phase 2: Score-based evaluation (UNCHANGED)
	evals := scoreStrategies(t, input.MemoryContext)

	// Phase 2.5: Adaptive adjustment (NEW)
	evals = applyAdaptiveAdjustments(evals, input.FailureStats)

	// Phase 3: Tiebreaker — uses AdjustedScore now
	selected := tiebreak(evals)

	role := defaultRoleMapping[selected.Strategy]

	reason := fmt.Sprintf("score-based: %s scored %.4f", selected.Strategy, selected.Score)
	if selected.AdaptiveAdjustment != nil {
		reason = fmt.Sprintf("[adaptive] score-based: %s scored %.4f → adjusted to %.4f (failure_rate=%.2f, confidence=%.2f)",
			selected.Strategy, selected.Score, selected.AdjustedScore,
			selected.AdaptiveAdjustment.FailureRate, selected.AdaptiveAdjustment.Confidence)
	}

	return EvaluatorResult{
		Evaluations:      evals,
		SelectedStrategy: selected.Strategy,
		SelectedRole:     role,
		Reason:           reason,
	}
}
```

- [ ] **Step 3: Modify tiebreaker to use AdjustedScore**

```go
func tiebreak(evals []StrategyEvaluation) StrategyEvaluation {
	simplicity := map[RoutingStrategy]int{
		StrategyDirect:    0,
		StrategyDecompose: 1,
		StrategyEscalate:  2,
	}

	best := evals[0]
	for _, e := range evals[1:] {
		if e.AdjustedScore > best.AdjustedScore {
			best = e
		} else if e.AdjustedScore == best.AdjustedScore && simplicity[e.Strategy] < simplicity[best.Strategy] {
			best = e
		}
	}
	return best
}
```

- [ ] **Step 4: Also set AdjustedScore in scoreStrategies**

In `scoreStrategies`, after computing `score`, also set `AdjustedScore = score` as the default (before adaptive adjustment):

```go
evals = append(evals, StrategyEvaluation{
	Strategy:      strategy,
	Score:         score,
	AdjustedScore: score, // default before adaptive
	FactorScores:  factors,
	Overridden:    false,
	Reason:        fmt.Sprintf("%s scored %.4f", strategy, score),
})
```

And in `overrideResult`, set `AdjustedScore: 1.0` on the override eval.

- [ ] **Step 5: Write evaluator adaptive tests**

Add to `internal/domain/routing/evaluator_test.go`:

```go
func TestEvaluate_WithAdaptiveAdjustment(t *testing.T) {
	tk := makeTask(t, task.TypeBugfix, task.ScopeFile, shared.RiskLow, nil)
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{
			{Strategy: StrategyDirect, Role: "implementer", Category: "tool"}: {Total: 40, Failures: 30, Rate: 0.75},
		},
		ByStrategyCategory: map[StatsKey]FailureRate{},
		ByStrategy:         map[RoutingStrategy]FailureRate{},
	}

	result := Evaluate(EvaluatorInput{Task: tk, FailureStats: stats})
	// Direct should have a penalty applied
	for _, eval := range result.Evaluations {
		if eval.Strategy == StrategyDirect {
			assert.Less(t, eval.AdjustedScore, eval.Score, "direct should be penalized")
			assert.NotNil(t, eval.AdaptiveAdjustment)
		}
	}
}

func TestEvaluate_WithoutStats_IdenticalToBaseline(t *testing.T) {
	tk := makeTask(t, task.TypeBugfix, task.ScopeFile, shared.RiskLow, nil)
	result := Evaluate(EvaluatorInput{Task: tk, FailureStats: nil})
	for _, eval := range result.Evaluations {
		assert.Equal(t, eval.Score, eval.AdjustedScore, "without stats, AdjustedScore should equal Score")
		assert.Nil(t, eval.AdaptiveAdjustment)
	}
}

func TestEvaluate_OverrideIgnoresAdaptive(t *testing.T) {
	tk := makeTask(t, task.TypeDevelopment, task.ScopeFile, shared.RiskCritical, nil)
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{},
		ByStrategyCategory:     map[StatsKey]FailureRate{},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyEscalate: {Total: 100, Failures: 90, Rate: 0.9},
		},
	}
	result := Evaluate(EvaluatorInput{Task: tk, FailureStats: stats})
	assert.Equal(t, StrategyEscalate, result.SelectedStrategy)
	assert.Contains(t, result.Reason, "[override]")
	// Override evals don't have AdaptiveAdjustment
	assert.Nil(t, result.Evaluations[0].AdaptiveAdjustment)
}

func TestEvaluate_AdaptiveCanChangeWinner(t *testing.T) {
	tk := makeTask(t, task.TypeResearch, task.ScopeModule, shared.RiskMedium, nil)
	// Heavy penalty on the current winner to see if adaptation shifts the result
	stats := &FailureStats{
		ByStrategyRoleCategory: map[StatsKey]FailureRate{},
		ByStrategyCategory:     map[StatsKey]FailureRate{},
		ByStrategy: map[RoutingStrategy]FailureRate{
			StrategyDirect:    {Total: 50, Failures: 45, Rate: 0.9},  // very high failure
			StrategyDecompose: {Total: 50, Failures: 2, Rate: 0.04},  // very low failure
		},
	}
	result := Evaluate(EvaluatorInput{Task: tk, FailureStats: stats})
	// With heavy penalty on direct and bonus on decompose, decompose may win
	assert.Contains(t, result.Reason, "[adaptive]")
}
```

- [ ] **Step 6: Run all routing tests**

Run: `go test ./internal/domain/routing/... -v -count=1`
Expected: ALL PASS (existing + new)

- [ ] **Step 7: Verify full build + existing tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 8: Commit**

```bash
git add internal/domain/routing/evaluator.go internal/domain/routing/evaluator_test.go
git commit -m "feat(adaptive): add Phase 2.5 adaptive adjustment to routing evaluator"
```

---

## Task 3: FailureStatsCollector + Store (B3)

**Files:**
- Create: `internal/application/routing/failure_stats_store.go`
- Create: `internal/application/routing/failure_stats_collector.go`
- Create: `internal/application/routing/failure_stats_collector_test.go`

- [ ] **Step 1: Implement FailureStatsStore**

```go
// internal/application/routing/failure_stats_store.go
package routing

import (
	"sync/atomic"

	domainrouting "github.com/russellcxl/agent-governance-core/internal/domain/routing"
)

// FailureStatsStore provides lock-free read access to the current FailureStats snapshot.
// Before the first refresh, Get() returns nil.
type FailureStatsStore struct {
	current atomic.Pointer[domainrouting.FailureStats]
}

// NewFailureStatsStore creates an empty store. Get() returns nil until Update is called.
func NewFailureStatsStore() *FailureStatsStore {
	return &FailureStatsStore{}
}

// Get returns the current FailureStats snapshot, or nil if not yet populated.
func (s *FailureStatsStore) Get() *domainrouting.FailureStats {
	return s.current.Load()
}

// Update atomically replaces the current snapshot.
func (s *FailureStatsStore) Update(stats *domainrouting.FailureStats) {
	s.current.Store(stats)
}
```

- [ ] **Step 2: Implement FailureStatsCollector**

```go
// internal/application/routing/failure_stats_collector.go
package routing

import (
	"context"
	"log/slog"
	"strings"
	"time"

	domainrouting "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

const (
	DefaultRefreshInterval = 5 * time.Minute
	DefaultStatsWindow     = 48 * time.Hour
)

// FailureStatsCollector periodically queries audit_entries and builds a FailureStats snapshot.
type FailureStatsCollector struct {
	store    *FailureStatsStore
	repo     outbound.AuditEntryRepository
	interval time.Duration
	window   time.Duration
	logger   *slog.Logger
}

// NewFailureStatsCollector creates a collector with the given dependencies.
func NewFailureStatsCollector(
	store *FailureStatsStore,
	repo outbound.AuditEntryRepository,
	interval time.Duration,
	window time.Duration,
	logger *slog.Logger,
) *FailureStatsCollector {
	return &FailureStatsCollector{
		store:    store,
		repo:     repo,
		interval: interval,
		window:   window,
		logger:   logger,
	}
}

// Start runs the collector in a loop. Blocks until ctx is cancelled.
func (c *FailureStatsCollector) Start(ctx context.Context) {
	c.refresh(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *FailureStatsCollector) refresh(ctx context.Context) {
	entries, _, err := c.repo.Query(ctx, outbound.AuditFilter{
		Action: strPtr("attempt_registered"),
		Limit:  10000,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failure stats refresh failed", "error", err)
		return
	}

	now := time.Now()
	cutoff := now.Add(-c.window)

	byStratRoleCat := make(map[domainrouting.StatsKey]domainrouting.FailureRate)
	byStratCat := make(map[domainrouting.StatsKey]domainrouting.FailureRate)
	byStrat := make(map[domainrouting.RoutingStrategy]domainrouting.FailureRate)

	for _, entry := range entries {
		if entry.CreatedAt().Time.Before(cutoff) {
			continue
		}

		actx := entry.Context()
		stratRaw, _ := actx["strategy_used"].(string)
		roleRaw, _ := actx["agent_role"].(string)
		codeRaw, _ := actx["failure_code"].(string)

		if stratRaw == "" {
			continue // skip entries without strategy
		}

		strategy := domainrouting.RoutingStrategy(stratRaw)
		category := extractCategory(codeRaw)
		isFail := entry.Outcome() == "failure" || entry.Outcome() == "retry"

		// Level 1: strategy × role × category
		if roleRaw != "" && category != "" {
			k := domainrouting.StatsKey{Strategy: strategy, Role: roleRaw, Category: category}
			fr := byStratRoleCat[k]
			fr.Total++
			if isFail {
				fr.Failures++
			}
			fr.Rate = float64(fr.Failures) / float64(fr.Total)
			byStratRoleCat[k] = fr
		}

		// Level 2: strategy × category
		if category != "" {
			k := domainrouting.StatsKey{Strategy: strategy, Category: category}
			fr := byStratCat[k]
			fr.Total++
			if isFail {
				fr.Failures++
			}
			fr.Rate = float64(fr.Failures) / float64(fr.Total)
			byStratCat[k] = fr
		}

		// Level 3: strategy only
		fr := byStrat[strategy]
		fr.Total++
		if isFail {
			fr.Failures++
		}
		fr.Rate = float64(fr.Failures) / float64(fr.Total)
		byStrat[strategy] = fr
	}

	c.store.Update(&domainrouting.FailureStats{
		ByStrategyRoleCategory: byStratRoleCat,
		ByStrategyCategory:     byStratCat,
		ByStrategy:             byStrat,
		ComputedAt:             now,
		Window:                 c.window,
	})

	c.logger.InfoContext(ctx, "failure stats refreshed",
		"strategies", len(byStrat),
		"window", c.window.String(),
	)
}

func extractCategory(code string) string {
	if i := strings.Index(code, "/"); i >= 0 {
		return code[:i]
	}
	return code
}

func strPtr(s string) *string { return &s }
```

- [ ] **Step 3: Write collector tests**

```go
// internal/application/routing/failure_stats_collector_test.go
package routing

import (
	"context"
	"testing"
	"time"
	"log/slog"

	domainaudit "github.com/russellcxl/agent-governance-core/internal/domain/audit"
	domainrouting "github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAuditRepo struct {
	entries []*domainaudit.AuditEntry
}

func (m *mockAuditRepo) Append(ctx context.Context, e *domainaudit.AuditEntry) error { return nil }
func (m *mockAuditRepo) Query(ctx context.Context, filter outbound.AuditFilter) ([]*domainaudit.AuditEntry, int, error) {
	return m.entries, len(m.entries), nil
}

func makeAuditEntry(strategy, role, code, outcome string) *domainaudit.AuditEntry {
	actx := domainaudit.NewAuditContext()
	if strategy != "" {
		actx = actx.WithStrategy(strategy)
	}
	if role != "" {
		actx = actx.WithAgentRole(role)
	}
	if code != "" {
		actx = actx.Set("failure_code", code)
	}
	entry, _ := domainaudit.NewAuditEntry(
		shared.AuditEntryID("01TESTENTRY00000000000000"),
		shared.ActorID("system"),
		"attempt_registered",
		outcome,
		actx,
		shared.MustTimestamp(time.Now()),
	)
	return entry
}

func TestStore_NilBeforeRefresh(t *testing.T) {
	store := NewFailureStatsStore()
	assert.Nil(t, store.Get())
}

func TestStore_AtomicUpdate(t *testing.T) {
	store := NewFailureStatsStore()
	stats := &domainrouting.FailureStats{ComputedAt: time.Now()}
	store.Update(stats)
	assert.NotNil(t, store.Get())
	assert.Equal(t, stats, store.Get())
}

func TestCollector_RefreshPopulatesAllLevels(t *testing.T) {
	repo := &mockAuditRepo{entries: []*domainaudit.AuditEntry{
		makeAuditEntry("direct", "implementer", "tool/shell_timeout", "failure"),
		makeAuditEntry("direct", "implementer", "tool/shell_timeout", "failure"),
		makeAuditEntry("direct", "implementer", "tool/git_push", "success"),
		makeAuditEntry("direct", "reviewer", "runtime/oom", "failure"),
		makeAuditEntry("decompose", "architect", "tool/shell_timeout", "success"),
		makeAuditEntry("decompose", "architect", "tool/shell_timeout", "success"),
	}}

	store := NewFailureStatsStore()
	collector := NewFailureStatsCollector(store, repo, time.Hour, 48*time.Hour, slog.Default())
	collector.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)

	// Level 3: strategy
	assert.Equal(t, 4, stats.ByStrategy[domainrouting.StrategyDirect].Total)
	assert.Equal(t, 3, stats.ByStrategy[domainrouting.StrategyDirect].Failures)
	assert.Equal(t, 2, stats.ByStrategy[domainrouting.StrategyDecompose].Total)
	assert.Equal(t, 0, stats.ByStrategy[domainrouting.StrategyDecompose].Failures)

	// Level 1: strategy × role × category
	k := domainrouting.StatsKey{Strategy: domainrouting.StrategyDirect, Role: "implementer", Category: "tool"}
	assert.Equal(t, 3, stats.ByStrategyRoleCategory[k].Total)
	assert.Equal(t, 2, stats.ByStrategyRoleCategory[k].Failures)
}

func TestCollector_EmptyResults(t *testing.T) {
	repo := &mockAuditRepo{entries: []*domainaudit.AuditEntry{}}
	store := NewFailureStatsStore()
	collector := NewFailureStatsCollector(store, repo, time.Hour, 48*time.Hour, slog.Default())
	collector.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats) // stats exists but empty
	assert.Empty(t, stats.ByStrategy)
}

func TestCollector_SkipsEntriesWithoutStrategy(t *testing.T) {
	repo := &mockAuditRepo{entries: []*domainaudit.AuditEntry{
		makeAuditEntry("", "implementer", "tool/x", "failure"), // no strategy
		makeAuditEntry("direct", "implementer", "tool/x", "success"),
	}}

	store := NewFailureStatsStore()
	collector := NewFailureStatsCollector(store, repo, time.Hour, 48*time.Hour, slog.Default())
	collector.refresh(context.Background())

	stats := store.Get()
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.ByStrategy[domainrouting.StrategyDirect].Total)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/application/routing/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/application/routing/failure_stats_store.go internal/application/routing/failure_stats_collector.go internal/application/routing/failure_stats_collector_test.go
git commit -m "feat(adaptive): add FailureStatsCollector and atomic store"
```

---

## Task 4: Config + Wiring + Route Task (B4)

**Files:**
- Modify: `internal/infrastructure/config/config.go`
- Modify: `internal/application/routing/route_task.go`
- Modify: `internal/bootstrap/wire.go`

- [ ] **Step 1: Add AdaptiveRoutingEnabled to config**

Modify `internal/infrastructure/config/config.go`:

Add to Config struct:
```go
AdaptiveRoutingEnabled bool
```

Add to Load():
```go
AdaptiveRoutingEnabled: envBool("ADAPTIVE_ROUTING_ENABLED", false),
```

Update doc comment to include `ADAPTIVE_ROUTING_ENABLED`.

- [ ] **Step 2: Add FailureStatsStore to RouteTaskService**

Modify `internal/application/routing/route_task.go`:

Add field to `RouteTaskService`:
```go
type RouteTaskService struct {
	// ... existing fields ...
	statsStore *FailureStatsStore // nil when adaptive routing disabled
}
```

Update `NewRouteTaskService` to accept `statsStore *FailureStatsStore` as the LAST parameter.

In `RouteTask` method, pass stats to evaluator:
```go
var failureStats *domainrouting.FailureStats
if s.statsStore != nil {
	failureStats = s.statsStore.Get()
}

result := domainrouting.Evaluate(domainrouting.EvaluatorInput{
	Task:          t,
	MemoryContext: memCtx,
	FailureStats:  failureStats,
})
```

- [ ] **Step 3: Update all callers of NewRouteTaskService**

Search for all callers and add `nil` (or the real store when wired):

```bash
rg "NewRouteTaskService\(" --files-with-matches
```

Update each caller: tests pass `nil`, bootstrap passes the real store when enabled.

- [ ] **Step 4: Wire collector in bootstrap**

Modify `internal/bootstrap/wire.go`:

When `cfg.AdaptiveRoutingEnabled`:
1. Create `FailureStatsStore`
2. Create `FailureStatsCollector`
3. Start collector in background goroutine
4. Pass store to `RouteTaskService`

```go
var statsStore *approuting.FailureStatsStore
if cfg.AdaptiveRoutingEnabled {
	statsStore = approuting.NewFailureStatsStore()
	collector := approuting.NewFailureStatsCollector(
		statsStore, auditRepo,
		approuting.DefaultRefreshInterval, approuting.DefaultStatsWindow,
		logger,
	)
	go collector.Start(ctx)
}

routeTaskSvc := approuting.NewRouteTaskService(taskRepo, routingRepo, wfRepo, &gen, clk, auditRecorder, memProvider, statsStore)
```

Add the collector context cancellation to App shutdown (or use the context passed to Wire).

- [ ] **Step 5: Verify build + all tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`
Expected: BUILD OK, ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/config/config.go internal/application/routing/route_task.go internal/bootstrap/wire.go
git commit -m "feat(adaptive): wire FailureStats into routing pipeline with ADAPTIVE_ROUTING_ENABLED flag"
```

---

## Task 5: Tracing + Persistence Update (B5)

**Files:**
- Modify: `internal/adapters/inbound/tracing/governance_traced.go`
- Modify: `internal/adapters/outbound/persistence/pg_routing_decision_repo.go` (verify JSONB works)

- [ ] **Step 1: Add adaptive trace attributes to RouteTask span**

Modify `governance_traced.go` — in the `RouteTask` method, after setting existing attributes, add:

```go
// Adaptive routing attributes
hasAdaptive := false
for _, eval := range result.EvaluatedStrategies() {
	if eval.AdaptiveAdjustment != nil {
		hasAdaptive = true
		break
	}
}
span.SetAttributes(attribute.Bool("governance.adaptive_applied", hasAdaptive))

if hasAdaptive {
	// Find the selected strategy's adjustment
	for _, eval := range result.EvaluatedStrategies() {
		if eval.Strategy == result.SelectedStrategy() && eval.AdaptiveAdjustment != nil {
			adj := eval.AdaptiveAdjustment
			span.SetAttributes(
				attribute.Float64("governance.adaptive_final_adjustment", adj.FinalAdjustment),
				attribute.Float64("governance.adaptive_failure_rate", adj.FailureRate),
				attribute.Float64("governance.adaptive_confidence", adj.Confidence),
				attribute.Int64("governance.adaptive_sample_size", int64(adj.SampleSize)),
				attribute.String("governance.adaptive_granularity", adj.Granularity),
			)
			break
		}
	}
}
```

- [ ] **Step 2: Verify JSONB serialization**

The `StrategyEvaluation` struct now has `AdjustedScore` and `AdaptiveAdjustment` fields with JSON tags. The PG repo already uses `json.Marshal(rd.EvaluatedStrategies())` for the JSONB column. The new fields will serialize automatically. **No code change needed** in the repo — just verify with a test.

Write a quick unit test to confirm JSON roundtrip:

```go
func TestStrategyEvaluation_JSONRoundtrip(t *testing.T) {
	eval := routing.StrategyEvaluation{
		Strategy:      routing.StrategyDirect,
		Score:         0.72,
		AdjustedScore: 0.6731,
		FactorScores:  map[string]float64{"risk_level": 0.9},
		AdaptiveAdjustment: &routing.AdaptiveAdjustment{
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

	var decoded routing.StrategyEvaluation
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, eval.AdjustedScore, decoded.AdjustedScore)
	assert.NotNil(t, decoded.AdaptiveAdjustment)
	assert.Equal(t, eval.AdaptiveAdjustment.Granularity, decoded.AdaptiveAdjustment.Granularity)
}
```

- [ ] **Step 3: Verify build + tests**

Run: `go build ./... && go test ./internal/... ./test/fixtures/... -count=1`

- [ ] **Step 4: Commit**

```bash
git add internal/adapters/inbound/tracing/governance_traced.go
git commit -m "feat(adaptive): add adaptive routing trace attributes and verify JSONB roundtrip"
```

---

## Task 6: Integration Test (B6)

**Files:**
- Create: `test/integration/adaptive/adaptive_routing_test.go`

- [ ] **Step 1: Write end-to-end adaptive routing test**

```go
//go:build integration

package adaptive_test

// Test plan:
// 1. Wire full stack with real PG + adaptive routing enabled
// 2. Submit + execute several tasks with various outcomes to build failure history
// 3. Manually trigger collector refresh
// 4. Route a new task
// 5. Verify RoutingDecision contains AdaptiveAdjustment data
// 6. Verify the adjustment matches the failure history

// Setup:
// - testhelpers.NewTestDB(t) for real PG
// - Wire services with statsStore
// - Create collector with short window
// - Execute: submit → route → policy → start → register attempt (mix of success/failure)
// - Refresh collector
// - Route another task and check the RoutingDecision
```

The test should:
1. Set up real PG with testcontainers
2. Wire all services manually (like the existing integration tests)
3. Create a `FailureStatsStore` and `FailureStatsCollector`
4. Execute 10+ tasks through the pipeline: some succeed, some fail with `tool/*` failures on `direct` strategy
5. Call `collector.refresh(ctx)` directly (don't wait for ticker)
6. Route a new task
7. Assert the RoutingDecision's evaluations contain `AdaptiveAdjustment` for `direct` strategy
8. Assert the adjustment is negative (penalty for high failure rate)
9. Assert the `Granularity` and `SampleSize` make sense

**READ these files before implementing:**
- `test/integration/usecases/process_task_test.go` — for wiring pattern
- `internal/application/routing/failure_stats_collector.go` — for the refresh method
- `internal/domain/routing/evaluator.go` — for EvaluatorInput with FailureStats

- [ ] **Step 2: Run integration test**

Run: `go test ./test/integration/adaptive/... -v -count=1 -tags=integration`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/adaptive/
git commit -m "feat(adaptive): add integration test for adaptive routing with real PG"
```

---

## Verification Checklist

After all tasks complete:

- [ ] `go build ./...` — SUCCESS
- [ ] `go test ./internal/... ./test/fixtures/... -count=1` — ALL PASS (zero regressions)
- [ ] `go test ./test/integration/... -v -count=1 -tags=integration` — ALL PASS
- [ ] `ADAPTIVE_ROUTING_ENABLED=false` → identical to v0.2.0 (all AdjustedScore == Score, no AdaptiveAdjustment)
- [ ] `ADAPTIVE_ROUTING_ENABLED=true` → collector runs, stats populated, adjustments applied
- [ ] Hard overrides still short-circuit (not affected by adaptive)
- [ ] Degradation cascade works: insufficient data at level 1 → level 2 → level 3 → nil
- [ ] Cold start: no stats → no adaptation → base routing
- [ ] Tracing: `governance.adaptive_applied` attribute present on RouteTask spans
- [ ] Persistence: JSONB in routing_decisions includes AdjustedScore + AdaptiveAdjustment
- [ ] No database schema changes
