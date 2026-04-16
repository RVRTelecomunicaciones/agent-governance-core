# Phase 2 Track 2: Adaptive Routing — Design Spec

**Date**: 2026-04-15
**Status**: Approved
**Scope**: Phase 2 Track 2
**Baseline**: v0.2.0 (Phase 1 + Track 1 Observability)
**Stack**: Go 1.26.2, existing routing evaluator, audit trail as data source

---

## 1. Objective

Add failure-history-driven score adjustments to the routing evaluator so that strategy selection adapts based on observed execution outcomes — automatically, within strict safety bounds, and with full explainability.

### What changes

The routing evaluator currently has 3 phases:

```
Phase 1: Hard overrides    → short-circuit if matched
Phase 2: Score-based eval  → weighted sum per strategy
Phase 3: Tiebreaker        → highest score, simplest on tie
```

Track 2 inserts **Phase 2.5: Adaptive adjustment** between scoring and tiebreaker:

```
Phase 1:   Hard overrides         → short-circuit (UNCHANGED)
Phase 2:   Score-based eval       → base scores (UNCHANGED)
Phase 2.5: Adaptive adjustment    → adjust scores based on failure history (NEW)
Phase 3:   Tiebreaker             → highest adjusted score, simplest on tie (uses AdjustedScore now)
```

### What does NOT change

- Hard overrides — evaluated before scoring, never affected by adaptation
- Base scoring formula — weights, factors, all unchanged
- Policy evaluation — downstream of routing, unaffected
- Workflow state machine — unaffected
- Domain model — no changes to core aggregates

---

## 2. Architecture

### Components

```
┌─────────────────────────────────────────────────────┐
│                  Routing Evaluator                    │
│                                                       │
│  Phase 1: Overrides (unchanged)                       │
│  Phase 2: Scoring (unchanged)                         │
│  Phase 2.5: Adaptive Adjustment ◄── FailureStats     │
│  Phase 3: Tiebreaker (uses AdjustedScore)            │
└─────────────────────────────────────────────────────┘
                                          ▲
                                          │ read (O(1))
                                          │
┌─────────────────────────────────────────┤
│           FailureStatsCollector          │
│                                          │
│  Background goroutine (every 5 min)      │
│  Queries audit_entries (48h window)      │
│  Aggregates by strategy×role×category    │
│  Stores snapshot in memory               │
└──────────────────────────────────────────┘
                    │
                    │ query
                    ▼
            ┌──────────────┐
            │ audit_entries │  (PostgreSQL)
            │  (append-only) │
            └──────────────┘
```

### Data flow

1. `FailureStatsCollector` runs as a background goroutine, started with the application
2. Every 5 minutes, it queries `audit_entries` for `action = 'attempt_registered'` within the last 48 hours
3. It aggregates failure rates by `strategy × agent_role × failure_category`
4. It stores the result as an atomic snapshot (`FailureStats`)
5. When `Evaluate()` runs, it reads the current `FailureStats` snapshot (O(1), no lock contention)
6. The adaptive phase computes adjustments per strategy, modulated by confidence
7. The adjusted scores are used by the tiebreaker to select the winning strategy
8. Everything is recorded in `AdaptiveAdjustment` struct for explainability

---

## 3. FailureStats Data Model

### Aggregate structure

```go
// FailureStats holds the in-memory failure rate snapshot.
type FailureStats struct {
    // Fine-grained: strategy × role × category
    ByStrategyRoleCategory map[StatsKey]FailureRate
    // Medium: strategy × category
    ByStrategyCategory map[StatsKey]FailureRate
    // Coarse: strategy only
    ByStrategy map[RoutingStrategy]FailureRate
    // Metadata
    ComputedAt time.Time
    Window     time.Duration // 48h
}

type StatsKey struct {
    Strategy RoutingStrategy
    Role     string // AgentRole as string, empty if not applicable at this level
    Category string // failure_category, empty if not applicable at this level
}

type FailureRate struct {
    Total    int     // total attempts
    Failures int     // failed attempts
    Rate     float64 // failures / total (0.0 - 1.0)
}
```

### Query for refresh

```sql
SELECT
    context->>'strategy_used' AS strategy,
    context->>'agent_role' AS agent_role,
    context->>'failure_stage' AS failure_stage,
    context->>'failure_code' AS failure_code,
    outcome
FROM audit_entries
WHERE action = 'attempt_registered'
  AND created_at > NOW() - INTERVAL '48 hours'
```

The collector processes each row:
- `outcome = 'success'` → counts as total, not as failure
- `outcome = 'failure'` or `outcome = 'retry'` → counts as total AND failure
- `failure_category` extracted from `failure_code` (prefix before `/`)
- `strategy_used` and `agent_role` may be null in old entries → skip those rows

The collector builds all three aggregation levels from the same query result in a single pass.

### Thread safety

The `FailureStats` snapshot is stored behind an `atomic.Pointer[FailureStats]`. The collector writes a new snapshot atomically. The evaluator reads the current pointer atomically. No mutexes needed — readers never block.

```go
type FailureStatsStore struct {
    current atomic.Pointer[FailureStats]
}

func (s *FailureStatsStore) Get() *FailureStats {
    return s.current.Load() // nil before first refresh
}

func (s *FailureStatsStore) Update(stats *FailureStats) {
    s.current.Store(stats)
}
```

---

## 4. FailureStatsCollector

### Lifecycle

```go
type FailureStatsCollector struct {
    store    *FailureStatsStore
    repo     outbound.AuditEntryRepository
    interval time.Duration // 5 minutes
    window   time.Duration // 48 hours
    logger   *slog.Logger
}

func (c *FailureStatsCollector) Start(ctx context.Context) {
    // Initial refresh
    c.refresh(ctx)
    
    // Periodic refresh
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
```

### Behavior

- **Cold start**: `store.Get()` returns `nil` until the first successful refresh. The evaluator checks for nil and skips adaptation.
- **Refresh success**: New `FailureStats` snapshot atomically replaces the old one.
- **Refresh failure**: Previous snapshot remains. Logger emits warning. Evaluator continues with stale data (better than no data). No retry — waits for next tick.
- **Shutdown**: Context cancellation stops the goroutine cleanly.

### Constants (Go code, configurable later)

```go
const (
    DefaultRefreshInterval = 5 * time.Minute
    DefaultStatsWindow     = 48 * time.Hour
)
```

---

## 5. Adaptive Adjustment — Position in Evaluator

### Modified evaluator flow

```go
func Evaluate(input EvaluatorInput) EvaluatorResult {
    // Phase 1: Hard overrides (UNCHANGED)
    if result, ok := checkOverrides(t); ok {
        return result
    }

    // Phase 2: Score-based evaluation (UNCHANGED)
    evals := scoreStrategies(t, input.MemoryContext)

    // Phase 2.5: Adaptive adjustment (NEW)
    evals = applyAdaptiveAdjustments(evals, input.FailureStats, input.Task)

    // Phase 3: Tiebreaker (uses AdjustedScore now)
    selected := tiebreak(evals) // MODIFIED: uses AdjustedScore instead of Score
    
    // ...
}
```

### EvaluatorInput change

```go
type EvaluatorInput struct {
    Task          *task.Task
    MemoryContext *MemoryContext
    FailureStats  *FailureStats  // NEW — nil means no adaptation
}
```

### applyAdaptiveAdjustments

```go
func applyAdaptiveAdjustments(evals []StrategyEvaluation, stats *FailureStats, t *task.Task) []StrategyEvaluation {
    for i := range evals {
        evals[i].AdjustedScore = evals[i].Score // default: no adjustment
        
        if stats == nil {
            continue // no stats → no adaptation
        }
        
        adjustment := computeAdjustment(evals[i].Strategy, stats, t)
        if adjustment != nil {
            evals[i].AdjustedScore = clamp(
                evals[i].Score + adjustment.FinalAdjustment,
                evals[i].Score - MaxPenalty,
                evals[i].Score + MaxBonus,
            )
            evals[i].AdaptiveAdjustment = adjustment
        }
    }
    return evals
}
```

---

## 6. Adjustment Formula — Complete

### Constants

```go
const (
    MaxPenalty           = 0.15
    MaxBonus             = 0.05
    BaselineFailureRate  = 0.20
    MinSamples           = 5
    FullConfidenceAt     = 30
)
```

### Degradation cascade

For a given strategy, the system tries to find failure rate data at decreasing granularity:

```
Level 1: strategy × agent_role × failure_category
Level 2: strategy × failure_category  
Level 3: strategy
Level 4: no adjustment
```

At each level, if `total >= MinSamples`, use that data. Otherwise, try the next coarser level.

The `agent_role` comes from the task's routing context — the default role for the strategy being evaluated. The `failure_category` is aggregated across all categories at levels 2 and 3.

```go
func computeAdjustment(strategy RoutingStrategy, stats *FailureStats, t *task.Task) *AdaptiveAdjustment {
    role := string(defaultRoleMapping[strategy])
    
    var rate float64
    var samples int
    var granularity string

    // Level 1: strategy × role — aggregate all categories for this strategy+role
    if fr := aggregateByStrategyAndRole(stats.ByStrategyRoleCategory, strategy, role); fr.Total >= MinSamples {
        rate = fr.Rate
        samples = fr.Total
        granularity = "strategy×role×category"
    // Level 2: strategy × category — aggregate all categories for this strategy (any role)
    } else if fr := aggregateByStrategy(stats.ByStrategyCategory, strategy); fr.Total >= MinSamples {
        rate = fr.Rate
        samples = fr.Total
        granularity = "strategy×category"
    // Level 3: strategy only
    } else if fr, ok := stats.ByStrategy[strategy]; ok && fr.Total >= MinSamples {
        rate = fr.Rate
        samples = fr.Total
        granularity = "strategy"
    } else {
        // Level 4: no adjustment — insufficient data at all levels
        return nil
    }
    
    // Compute raw adjustment
    raw := computeRawAdjustment(rate)
    
    // Compute confidence
    confidence := computeConfidence(samples)
    
    // Final adjustment
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

// aggregateByStrategy sums all FailureRate entries matching strategy across all roles and categories.
func aggregateByStrategy(m map[StatsKey]FailureRate, strategy RoutingStrategy) FailureRate {
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
```

### Raw adjustment calculation

```go
func computeRawAdjustment(failureRate float64) float64 {
    if failureRate > BaselineFailureRate {
        // Penalize — strategy fails more than expected
        excess := failureRate - BaselineFailureRate
        normalized := math.Min(excess/(1-BaselineFailureRate), 1.0)
        return -normalized * MaxPenalty
    }
    if failureRate < BaselineFailureRate {
        // Bonus — strategy fails less than expected
        deficit := BaselineFailureRate - failureRate
        normalized := math.Min(deficit/BaselineFailureRate, 1.0)
        return normalized * MaxBonus
    }
    return 0 // exactly at baseline
}
```

### Confidence calculation

```go
func computeConfidence(samples int) float64 {
    if samples <= MinSamples {
        return 0.0 // at min_samples, confidence is exactly 0
    }
    conf := float64(samples-MinSamples) / float64(FullConfidenceAt-MinSamples)
    return math.Min(conf, 1.0)
}
```

### Clamp

```go
func clamp(value, min, max float64) float64 {
    if value < min {
        return min
    }
    if value > max {
        return max
    }
    return value
}
```

### Complete example

```
Strategy: direct
Failure rate (strategy×role×category): 0.45 (18 failures / 40 attempts)
Samples: 40

Raw adjustment:
  excess = 0.45 - 0.20 = 0.25
  normalized = min(0.25 / 0.80, 1.0) = 0.3125
  raw = -0.3125 × 0.15 = -0.04688

Confidence:
  conf = min((40 - 5) / (30 - 5), 1.0) = min(1.4, 1.0) = 1.0

Final adjustment: -0.04688 × 1.0 = -0.04688

Base score: 0.72
Adjusted score: clamp(0.72 - 0.04688, 0.72 - 0.15, 0.72 + 0.05) = 0.6731
```

---

## 7. Explainability

### StrategyEvaluation — updated VO

```go
type AdaptiveAdjustment struct {
    RawAdjustment   float64 // before confidence modulation
    Confidence      float64 // 0.0-1.0
    FinalAdjustment float64 // raw × confidence, clamped
    FailureRate     float64 // observed rate
    SampleSize      int     // samples used
    Granularity     string  // "strategy×role×category", "strategy×category", "strategy"
    Window          string  // "48h"
}

type StrategyEvaluation struct {
    Strategy           RoutingStrategy
    Score              float64              // base score (UNCHANGED, always present)
    AdjustedScore      float64              // final score used for selection
    FactorScores       map[string]float64
    Overridden         bool
    Reason             string
    AdaptiveAdjustment *AdaptiveAdjustment  // nil if no adaptation applied
}
```

- `Score` = pure base score from weighted sum (auditable baseline)
- `AdjustedScore` = Score + FinalAdjustment (used by tiebreaker)
- `AdaptiveAdjustment` = nil when no adaptation (insufficient data, cold start, override)
- `Granularity` uses normalized values: `"strategy×role×category"`, `"strategy×category"`, `"strategy"`
- `Window` uses normalized value: `"48h"`

### RoutingDecision.Reason

When adaptation is applied, the Reason includes `[adaptive]` tag:

```
"[adaptive] score-based: direct scored 0.7200 → adjusted to 0.6731 (failure_rate=0.45, confidence=1.00)"
```

The `[adaptive]` tag is a human-readable marker. The structured `AdaptiveAdjustment` in the `StrategyEvaluation` is the source of truth.

### Persistence

The `RoutingDecision` is persisted with `evaluated_strategies` as JSONB. The `AdaptiveAdjustment` struct serializes naturally into the existing JSONB column — no schema change needed. The `AdjustedScore` field is also stored.

### Tracing

The RouteTask tracing decorator (from Track 1) already emits `governance.strategy` attribute. Track 2 adds these attributes when adaptation is applied:

| Attribute | Type | Present when |
|---|---|---|
| `governance.adaptive_applied` | bool | Always |
| `governance.adaptive_final_adjustment` | float64 | When applied |
| `governance.adaptive_failure_rate` | float64 | When applied |
| `governance.adaptive_confidence` | float64 | When applied |
| `governance.adaptive_sample_size` | int | When applied |
| `governance.adaptive_granularity` | string | When applied |

Omit attributes when adaptation is not applied (don't set to zero/empty).

### Audit

The `RouteTask` audit entry already records the routing decision. The `AdaptiveAdjustment` data is visible through the persisted `RoutingDecision`. No additional audit entries needed — the existing one carries all the information.

---

## 8. Safety Invariants

| Invariant | Mechanism |
|---|---|
| Hard overrides never affected | Overrides evaluated in Phase 1, before scoring. Phase 2.5 only runs when scoring runs. |
| Adjustment never dominates | MaxPenalty=0.15, MaxBonus=0.05. Base score (0.0-1.0) always the primary signal. |
| Bonus is bounded and cannot dominate | MaxBonus=0.05 is bounded and should not overcome significant base score differences. |
| No adjustment on insufficient data | MinSamples=5, confidence=0 at threshold, degradation cascade, nil FailureStats = no adjustment. |
| Cold start safe | Store returns nil before first refresh → evaluator skips adaptation entirely. |
| Refresh failure safe | Previous snapshot remains. Stale data with some confidence > no data. |
| Security decisions downstream unaffected | Policy evaluation runs after routing. If routing shifts to a different strategy, policy still evaluates and can deny/require approval. |
| Fully explainable | Every adjustment recorded with failure rate, confidence, sample size, granularity, window. |

---

## 9. Configuration

### New environment variables

| Variable | Default | Description |
|---|---|---|
| `ADAPTIVE_ROUTING_ENABLED` | `false` | Enable/disable adaptive routing |

All other parameters are Go constants in the first version:

| Constant | Value | Description |
|---|---|---|
| `MaxPenalty` | `0.15` | Maximum score penalty |
| `MaxBonus` | `0.05` | Maximum score bonus |
| `BaselineFailureRate` | `0.20` | Neutral failure rate |
| `MinSamples` | `5` | Minimum samples to activate (confidence=0 at this point) |
| `FullConfidenceAt` | `30` | Samples for full confidence |
| `DefaultRefreshInterval` | `5m` | Background refresh interval |
| `DefaultStatsWindow` | `48h` | Time window for failure rates |

When `ADAPTIVE_ROUTING_ENABLED=false`:
- `FailureStatsCollector` is not started
- `FailureStats` is always nil
- Evaluator runs Phase 1 → Phase 2 → Phase 3 (identical to v0.2.0)

---

## 10. File Structure

### New files

```
internal/
  domain/
    routing/
      adaptive.go                    — AdaptiveAdjustment VO, adjustment formula, confidence, clamp
      adaptive_test.go               — formula tests, confidence ramp, degradation, edge cases
      failure_stats.go               — FailureStats, FailureRate, StatsKey types
  application/
    routing/
      failure_stats_collector.go     — FailureStatsCollector (background goroutine + query)
      failure_stats_collector_test.go
      failure_stats_store.go         — FailureStatsStore (atomic pointer)
  infrastructure/
    config/
      config.go                      — MODIFY: add AdaptiveRoutingEnabled
  bootstrap/
    wire.go                          — MODIFY: start collector, pass FailureStats to evaluator
  cmd/
    agent-governance-core/
      main.go                        — MODIFY: lifecycle (no change if disabled)
```

### Modified files

| File | Change |
|---|---|
| `internal/domain/routing/evaluator.go` | Add Phase 2.5, modify tiebreaker to use AdjustedScore |
| `internal/domain/routing/evaluator_test.go` | Add tests for adaptive phase |
| `internal/domain/routing/strategy.go` | Add AdjustedScore + AdaptiveAdjustment to StrategyEvaluation |
| `internal/application/routing/route_task.go` | Pass FailureStats to Evaluate() |
| `internal/adapters/inbound/tracing/governance_traced.go` | Add adaptive trace attributes |
| `internal/adapters/outbound/persistence/pg_routing_decision_repo.go` | Serialize AdjustedScore + AdaptiveAdjustment in JSONB (no schema change) |
| `internal/infrastructure/config/config.go` | Add `AdaptiveRoutingEnabled` |
| `internal/bootstrap/wire.go` | Conditional collector start + FailureStats injection |

### NOT modified

- `internal/domain/` (except routing) — untouched
- `internal/ports/` — no new port interfaces needed
- `migrations/` — no schema changes (JSONB absorbs new fields)

### Architectural note: FailureStatsCollector and AuditEntryRepository

The `FailureStatsCollector` queries `AuditEntryRepository` directly — it does NOT go through an inbound port or a dedicated outbound port. This is a **pragmatic Track 2 decision**: the collector is an infrastructure-level read model builder, not a business use case. Creating a dedicated port for "query failure stats from audit" would add indirection without value at this stage. If the data source changes in the future (e.g. a dedicated metrics store or memory-engine), the collector is the only component to update.

---

## 11. Testing Strategy

### Domain Unit Tests (adaptive.go)

| Test | What it verifies |
|---|---|
| RawAdjustment at baseline | failure_rate=0.20 → adjustment=0 |
| RawAdjustment above baseline | failure_rate=0.45 → negative adjustment within [-0.15, 0] |
| RawAdjustment below baseline | failure_rate=0.05 → positive adjustment within [0, 0.05] |
| RawAdjustment at extremes | failure_rate=1.0 → -0.15, failure_rate=0.0 → +0.05 |
| Confidence at min_samples | samples=5 → confidence=0.0 |
| Confidence above min | samples=10 → confidence=0.2 |
| Confidence at full | samples=30 → confidence=1.0 |
| Confidence above full | samples=100 → confidence=1.0 (capped) |
| FinalAdjustment = raw × confidence | Verify multiplication |
| Clamp within bounds | adjusted score never exceeds base ± bounds |
| Degradation Level 1 → 2 → 3 → nil | Insufficient samples at each level triggers fallback |
| No adjustment when stats nil | FailureStats=nil → all AdjustedScore == Score |

### Evaluator Integration Tests

| Test | What it verifies |
|---|---|
| Evaluate with adaptive adjustment | Base score modified by failure history |
| Evaluate without stats (nil) | Identical to v0.2.0 behavior |
| Override + adaptive | Override still short-circuits, adaptive not applied |
| Tiebreaker uses AdjustedScore | When adaptation changes the winner |
| Adaptation doesn't flip secure decisions | High-risk task still goes to appropriate strategy |

### FailureStatsCollector Tests

| Test | What it verifies |
|---|---|
| Refresh populates all 3 aggregation levels | query result → ByStrategy, ByStrategyCategory, ByStrategyRoleCategory |
| Refresh with empty results | Stats has zero rates, not nil |
| Cold start — Get returns nil | Before first refresh, store is nil |
| Atomic update — concurrent reads safe | Multiple goroutines read while collector writes |
| Collector respects context cancellation | Stops cleanly on shutdown |

### Integration Test (with real PG)

- Submit + execute several tasks with different outcomes (success, failure with various failure_codes)
- Wait for collector refresh
- Route a new task
- Verify RoutingDecision contains AdaptiveAdjustment data
- Verify trace span has adaptive attributes

---

## 12. Implementation Blocks

| Block | Depends on | Scope |
|---|---|---|
| **B1: Adaptive domain** | Nothing | `routing/adaptive.go`, `routing/failure_stats.go`, VO updates |
| **B2: Evaluator modification** | B1 | `routing/evaluator.go` Phase 2.5 + tiebreaker change |
| **B3: FailureStatsCollector** | B1 | `application/routing/failure_stats_*.go` |
| **B4: Config + wiring** | B1-B3 | `config.go`, `wire.go`, `main.go`, `route_task.go` |
| **B5: Tracing + persistence** | B1, B4 | Trace attributes, JSONB serialization update |
| **B6: Tests** | B1-B5 | Domain tests, evaluator tests, collector tests, integration test |

### Parallelization

```
B1 (adaptive domain + types)
 ├── B2 (evaluator modification) ─── parallel
 └── B3 (collector) ──────────────── parallel
         │
         B4 (config + wiring)
         │
         B5 (tracing + persistence)
         │
         B6 (tests)
```

B2 and B3 can run in parallel after B1. B4-B6 are sequential.

---

## 13. Baseline Invariants (must not break)

- All existing tests continue to pass
- `ADAPTIVE_ROUTING_ENABLED=false` (default) produces identical behavior to v0.2.0
- Hard overrides unaffected
- Base scoring formula unchanged
- Policy evaluation unaffected
- No database schema changes
- `go build ./...` succeeds
- Binary starts and works without adaptive routing configured

---

## Appendix: Decisions Summary

| # | Decision | Rationale |
|---|---|---|
| D1 | Automatic bounded adaptivity | Real impact with safety bounds, not just recommendations |
| D2 | In-memory aggregate, not direct DB query | O(1) reads, no coupling evaluator↔persistence |
| D3 | strategy × role × category with 4-level degradation | Fine granularity with graceful fallback on insufficient data |
| D4 | Bounded additive clamp: penalty=0.15, bonus=0.05 | Asymmetric — penalize failures harder. Base score stays primary signal. |
| D5 | Baseline failure rate = 0.20 | Clear neutral point, proportional adjustment |
| D6 | Confidence: min=5 (conf=0), full=30 (conf=1.0), linear | Gradual ramp prevents noise-based adjustments |
| D7 | Background refresh every 5 minutes | Async, no latency in routing path, resilient to failures |
| D8 | Fixed 48h window | Simple, good balance. Cascading windows deferred. |
| D9 | Score/AdjustedScore/AdaptiveAdjustment explainability | Full transparency: what, why, with what evidence |
| D10 | ADAPTIVE_ROUTING_ENABLED flag | Backwards compatible, zero impact when disabled |
