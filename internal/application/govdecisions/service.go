// Package govdecisions implements the orchestator-facing governance facade
// surfaced under /governance/v1/decisions/... — the endpoints the
// sophia-orchestator hits before running each SDD phase (Iron Law #4).
//
// V1 contract (M-E0): default-allow with persistent audit trail. Every
// EvaluatePhase / EvaluateSensitive call writes a phase_decisions row; every
// approval poll consults phase_approvals. The "real" policy engine remains in
// the policyeval service (/api/v1/tasks/{id}/evaluate-policy) — Sprint 3 will
// unify both surfaces. See ADR-0003 and M-E0 design notes.
package govdecisions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// DecisionType mirrors the orchestator wire enum.
type DecisionType string

const (
	DecisionAllow            DecisionType = "allow"
	DecisionDeny             DecisionType = "deny"
	DecisionRequireApproval  DecisionType = "require_approval"
	defaultAgentRolePhase                 = "team-lead"
	defaultReasonPhase                    = "M-E0 default-allow policy"
	defaultReasonSensitive                = "M-E0 default-allow sensitive policy"
)

// ApprovalStatus mirrors the orchestator wire enum.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalGranted  ApprovalStatus = "granted"
	ApprovalDenied   ApprovalStatus = "denied"
)

// PhaseDecisionInput is what the HTTP layer hands the service.
type PhaseDecisionInput struct {
	ChangeID        string
	PhaseType       string
	TaskDescription string
	Sensitive       bool
}

// SensitiveDecisionInput mirrors sensitive-capability evaluation.
type SensitiveDecisionInput struct {
	ChangeID   string
	Capability string
	Payload    []byte
}

// Decision is the result returned to callers.
type Decision struct {
	ID          string
	Decision    DecisionType
	AgentRole   string
	Strategy    string
	Reason      string
	Constraints map[string]any
}

// ErrInvalidInput is returned for validation failures the handler should map to 400.
var ErrInvalidInput = errors.New("invalid input")

// Logger is the minimal interface DecisionsService needs (decoupled from slog
// for testability and to avoid pulling slog into the application layer).
type Logger interface {
	Warn(msg string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Warn(string, ...any) {}

// DecisionsService implements the M-E0 default-allow policy and writes audit rows.
type DecisionsService struct {
	decisions outbound.PhaseDecisionRepository
	approvals outbound.PhaseApprovalRepository
	gen       IDGen
	clk       Clock
	log       Logger
}

// IDGen lets the service mint decision IDs without leaking infrastructure.
type IDGen interface {
	NewPhaseDecisionID() string
}

// Clock returns the current time. We use time.Time directly here (not the
// shared.Timestamp domain VO) because phase decisions are an orchestator-facing
// audit surface — keeping it primitive avoids coupling this facade to the
// internal policy domain. Sprint 3 unification can promote a domain aggregate.
type Clock interface {
	NowUTC() time.Time
}

// NewDecisionsService wires the service.
func NewDecisionsService(
	decisions outbound.PhaseDecisionRepository,
	approvals outbound.PhaseApprovalRepository,
	gen IDGen,
	clk Clock,
	log Logger,
) *DecisionsService {
	if log == nil {
		log = noopLogger{}
	}
	return &DecisionsService{
		decisions: decisions,
		approvals: approvals,
		gen:       gen,
		clk:       clk,
		log:       log,
	}
}

// EvaluatePhase validates, persists, and returns the V1 default-allow phase decision.
func (s *DecisionsService) EvaluatePhase(ctx context.Context, in PhaseDecisionInput) (*Decision, error) {
	if in.ChangeID == "" {
		return nil, fmt.Errorf("%w: change_id is required", ErrInvalidInput)
	}
	if in.PhaseType == "" {
		return nil, fmt.Errorf("%w: phase_type is required", ErrInvalidInput)
	}

	if in.Sensitive {
		s.log.Warn("phase decision flagged sensitive (M-E0 default-allow still applies)",
			"change_id", in.ChangeID, "phase_type", in.PhaseType)
	}

	rec := outbound.PhaseDecisionRecord{
		ID:        s.gen.NewPhaseDecisionID(),
		ChangeID:  in.ChangeID,
		PhaseType: in.PhaseType,
		Sensitive: in.Sensitive,
		Decision:  string(DecisionAllow),
		AgentRole: defaultAgentRolePhase,
		Reason:    defaultReasonPhase,
		CreatedAt: s.clk.NowUTC(),
	}
	if err := s.decisions.Save(ctx, rec); err != nil {
		return nil, fmt.Errorf("persisting phase decision: %w", err)
	}
	return &Decision{
		ID:        rec.ID,
		Decision:  DecisionAllow,
		AgentRole: defaultAgentRolePhase,
		Reason:    defaultReasonPhase,
	}, nil
}

// EvaluateSensitive validates, persists, and returns the V1 default-allow sensitive decision.
func (s *DecisionsService) EvaluateSensitive(ctx context.Context, in SensitiveDecisionInput) (*Decision, error) {
	if in.ChangeID == "" {
		return nil, fmt.Errorf("%w: change_id is required", ErrInvalidInput)
	}
	if in.Capability == "" {
		return nil, fmt.Errorf("%w: capability is required", ErrInvalidInput)
	}

	s.log.Warn("sensitive capability evaluated (M-E0 default-allow)",
		"change_id", in.ChangeID, "capability", in.Capability)

	rec := outbound.PhaseDecisionRecord{
		ID:         s.gen.NewPhaseDecisionID(),
		ChangeID:   in.ChangeID,
		PhaseType:  "sensitive",
		Capability: in.Capability,
		Sensitive:  true,
		Decision:   string(DecisionAllow),
		Reason:     defaultReasonSensitive,
		CreatedAt:  s.clk.NowUTC(),
	}
	if err := s.decisions.Save(ctx, rec); err != nil {
		return nil, fmt.Errorf("persisting sensitive decision: %w", err)
	}
	return &Decision{
		ID:       rec.ID,
		Decision: DecisionAllow,
		Reason:   defaultReasonSensitive,
	}, nil
}

// ApprovalStatusFor returns the status row for (changeID, phaseID). When no
// row exists the orchestator treats the gate as auto-granted — V1 default.
func (s *DecisionsService) ApprovalStatusFor(ctx context.Context, changeID, phaseID string) (ApprovalStatus, error) {
	if changeID == "" || phaseID == "" {
		return "", fmt.Errorf("%w: change_id and phase_id are required", ErrInvalidInput)
	}
	rec, err := s.approvals.Find(ctx, changeID, phaseID)
	if err != nil {
		return "", fmt.Errorf("looking up phase approval: %w", err)
	}
	if rec == nil {
		// V1: no explicit gate → granted by default.
		return ApprovalGranted, nil
	}
	return ApprovalStatus(rec.Status), nil
}
