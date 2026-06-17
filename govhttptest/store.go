// Package govhttptest is an EXPORTED test-support seam (not under internal/) so
// cross-module consumers — e.g. sophia-orchestator — can stand up the REAL
// /governance/v1/* decision facade in-process for hermetic contract tests.
//
// It wires the production chi router (internal/adapters/inbound/http) and the
// production govdecisions.DecisionsService (internal/application/govdecisions)
// against IN-MEMORY implementations of the outbound repositories the service
// needs, plus a deterministic id generator and clock. No PostgreSQL, no
// network, no time.Now/ulid.Make — the seam is fully reproducible.
//
// This package is intended for tests only. It is public solely because Go
// forbids importing another module's internal/ packages; keeping the surface
// minimal (a wired http.Handler plus an inspectable store) avoids leaking the
// internal facade types to consumers.
package govhttptest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/govdecisions"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
)

// Compile-time assertions that the in-memory fakes satisfy the REAL production
// interfaces. These localize a future break to this file (with a clear message)
// rather than at a distant constructor call — which is exactly the contract
// drift this seam exists to catch.
var (
	_ outbound.PhaseDecisionRepository = (*inMemoryDecisionRepo)(nil)
	_ outbound.PhaseApprovalRepository = (*inMemoryApprovalRepo)(nil)
	_ govdecisions.IDGen               = (*deterministicIDGen)(nil)
	_ govdecisions.Clock               = fixedClock{}
)

// fixedInstant is the deterministic timestamp every in-memory clock returns.
// A fixed UTC instant keeps recorded audit rows reproducible across runs.
var fixedInstant = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// deterministicIDGen mints stable, sequential phase-decision ids without
// ulid.Make so contract tests stay reproducible. It satisfies
// govdecisions.IDGen.
type deterministicIDGen struct {
	mu   sync.Mutex
	next int
}

// NewPhaseDecisionID returns a stable id of the form "DET-PHASE-DECISION-000N".
// The first id from a fresh generator is always identical.
func (g *deterministicIDGen) NewPhaseDecisionID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return fmt.Sprintf("DET-PHASE-DECISION-%04d", g.next)
}

// fixedClock returns a fixed UTC instant. It satisfies govdecisions.Clock.
type fixedClock struct{}

// NowUTC returns the deterministic fixed instant.
func (fixedClock) NowUTC() time.Time { return fixedInstant }

// inMemoryDecisionRepo is an in-memory outbound.PhaseDecisionRepository. It
// records every saved decision row so tests can assert persistence happened.
type inMemoryDecisionRepo struct {
	mu    sync.Mutex
	saved []outbound.PhaseDecisionRecord
}

// Save appends the decision record to the in-memory log.
func (r *inMemoryDecisionRepo) Save(_ context.Context, rec outbound.PhaseDecisionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saved = append(r.saved, rec)
	return nil
}

// Saved returns a copy of the recorded decision rows.
func (r *inMemoryDecisionRepo) Saved() []outbound.PhaseDecisionRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]outbound.PhaseDecisionRecord, len(r.saved))
	copy(out, r.saved)
	return out
}

// inMemoryApprovalRepo is an in-memory outbound.PhaseApprovalRepository. With no
// seeded rows it always returns (nil, nil), which the service interprets as the
// V1 default-allow "auto-granted" gate — matching production behaviour when no
// explicit approval gate exists.
type inMemoryApprovalRepo struct {
	mu   sync.Mutex
	rows map[string]*outbound.PhaseApprovalRecord
}

func newInMemoryApprovalRepo() *inMemoryApprovalRepo {
	return &inMemoryApprovalRepo{rows: make(map[string]*outbound.PhaseApprovalRecord)}
}

func approvalKey(changeID, phaseID string) string { return changeID + "\x00" + phaseID }

// Find returns the seeded approval row for (changeID, phaseID), or (nil, nil)
// when none exists.
func (r *inMemoryApprovalRepo) Find(_ context.Context, changeID, phaseID string) (*outbound.PhaseApprovalRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[approvalKey(changeID, phaseID)], nil
}

// Seed registers an explicit approval gate so consumers can exercise
// non-default approval statuses if needed.
func (r *inMemoryApprovalRepo) Seed(rec outbound.PhaseApprovalRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[approvalKey(rec.ChangeID, rec.PhaseID)] = &rec
}

// Store bundles the deterministic id/clock and in-memory repositories used to
// wire the facade. Exposed so contract tests can inspect recorded rows and seed
// approval gates without reaching into internal packages.
type Store struct {
	IDGen     *deterministicIDGen
	Clock     fixedClock
	Decisions *inMemoryDecisionRepo
	Approvals *inMemoryApprovalRepo
}

// NewInMemoryStore builds a fresh deterministic store. Two stores constructed
// independently produce identical first ids and the same fixed clock instant.
func NewInMemoryStore() *Store {
	return &Store{
		IDGen:     &deterministicIDGen{},
		Clock:     fixedClock{},
		Decisions: &inMemoryDecisionRepo{},
		Approvals: newInMemoryApprovalRepo(),
	}
}
