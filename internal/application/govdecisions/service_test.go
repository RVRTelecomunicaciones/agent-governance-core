package govdecisions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// --- Mocks ---

type fakeDecisionRepo struct {
	saved []outbound.PhaseDecisionRecord
	err   error
}

func (f *fakeDecisionRepo) Save(_ context.Context, rec outbound.PhaseDecisionRecord) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, rec)
	return nil
}

type fakeApprovalRepo struct {
	rec *outbound.PhaseApprovalRecord
	err error
}

func (f *fakeApprovalRepo) Find(_ context.Context, _, _ string) (*outbound.PhaseApprovalRecord, error) {
	return f.rec, f.err
}

type fixedID struct{ id string }

func (f fixedID) NewPhaseDecisionID() string { return f.id }

type fixedClock struct{ t time.Time }

func (f fixedClock) NowUTC() time.Time { return f.t }

// --- Tests ---

func TestEvaluatePhase_PersistsAndReturnsAllow(t *testing.T) {
	repo := &fakeDecisionRepo{}
	svc := NewDecisionsService(repo, &fakeApprovalRepo{}, fixedID{id: "01ID00000000000000000000PD"},
		fixedClock{t: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)}, nil)

	got, err := svc.EvaluatePhase(context.Background(), PhaseDecisionInput{
		ChangeID: "01KRJB5JRS86FCC9Y7DCDZP7X3", PhaseType: "apply",
	})

	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, got.Decision)
	assert.Equal(t, "team-lead", got.AgentRole)
	require.Len(t, repo.saved, 1)
	saved := repo.saved[0]
	assert.Equal(t, "01ID00000000000000000000PD", saved.ID)
	assert.Equal(t, "01KRJB5JRS86FCC9Y7DCDZP7X3", saved.ChangeID)
	assert.Equal(t, "apply", saved.PhaseType)
	assert.Equal(t, "allow", saved.Decision)
}

func TestEvaluatePhase_RejectsEmptyChangeID(t *testing.T) {
	svc := NewDecisionsService(&fakeDecisionRepo{}, &fakeApprovalRepo{},
		fixedID{id: "id"}, fixedClock{t: time.Now()}, nil)
	_, err := svc.EvaluatePhase(context.Background(), PhaseDecisionInput{PhaseType: "explore"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidInput))
}

func TestEvaluatePhase_RejectsEmptyPhaseType(t *testing.T) {
	svc := NewDecisionsService(&fakeDecisionRepo{}, &fakeApprovalRepo{},
		fixedID{id: "id"}, fixedClock{t: time.Now()}, nil)
	_, err := svc.EvaluatePhase(context.Background(), PhaseDecisionInput{ChangeID: "cid"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidInput))
}

func TestEvaluateSensitive_PersistsCapabilityAndAllows(t *testing.T) {
	repo := &fakeDecisionRepo{}
	svc := NewDecisionsService(repo, &fakeApprovalRepo{}, fixedID{id: "01SENS"},
		fixedClock{t: time.Now()}, nil)

	got, err := svc.EvaluateSensitive(context.Background(), SensitiveDecisionInput{
		ChangeID: "01KRJB5JRS86FCC9Y7DCDZP7X3", Capability: "shell.exec@v1",
	})

	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, got.Decision)
	require.Len(t, repo.saved, 1)
	assert.Equal(t, "shell.exec@v1", repo.saved[0].Capability)
	assert.True(t, repo.saved[0].Sensitive)
}

func TestApprovalStatusFor_NoRowReturnsGranted(t *testing.T) {
	svc := NewDecisionsService(&fakeDecisionRepo{}, &fakeApprovalRepo{rec: nil},
		fixedID{id: "id"}, fixedClock{t: time.Now()}, nil)
	status, err := svc.ApprovalStatusFor(context.Background(), "cid", "pid")
	require.NoError(t, err)
	assert.Equal(t, ApprovalGranted, status)
}

func TestApprovalStatusFor_PendingRowPropagates(t *testing.T) {
	approvals := &fakeApprovalRepo{rec: &outbound.PhaseApprovalRecord{Status: "pending"}}
	svc := NewDecisionsService(&fakeDecisionRepo{}, approvals,
		fixedID{id: "id"}, fixedClock{t: time.Now()}, nil)
	status, err := svc.ApprovalStatusFor(context.Background(), "cid", "pid")
	require.NoError(t, err)
	assert.Equal(t, ApprovalPending, status)
}
