package inbound

import (
	"context"

	"github.com/russellcxl/agent-governance-core/internal/domain/approval"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// ResolveApprovalInput holds the data required to resolve an approval request.
type ResolveApprovalInput struct {
	ApprovalRequestID shared.ApprovalRequestID
	Approved          bool
	ResolvedBy        shared.ActorID
	Reason            string
}

// ApprovalService defines the HITL approval interface.
type ApprovalService interface {
	ResolveApproval(ctx context.Context, input ResolveApprovalInput) (*approval.ApprovalRequest, error)
	GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error)
}
