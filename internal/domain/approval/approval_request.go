package approval

import (
	"errors"
	"fmt"
	"strings"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// Errors for the approval aggregate.
var (
	ErrEmptyReason   = errors.New("approval reason must not be empty")
	ErrNotPending    = errors.New("approval request is not in pending status")
)

// --- ApprovalStatus enum ---

// ApprovalStatus represents the lifecycle state of an approval request.
type ApprovalStatus string

const (
	StatusPending  ApprovalStatus = "pending"
	StatusApproved ApprovalStatus = "approved"
	StatusDenied   ApprovalStatus = "denied"
	StatusExpired  ApprovalStatus = "expired"
)

var validApprovalStatuses = map[ApprovalStatus]bool{
	StatusPending:  true,
	StatusApproved: true,
	StatusDenied:   true,
	StatusExpired:  true,
}

// NewApprovalStatus validates and returns an ApprovalStatus.
func NewApprovalStatus(s string) (ApprovalStatus, error) {
	as := ApprovalStatus(s)
	if !validApprovalStatuses[as] {
		return "", fmt.Errorf("invalid approval status: %q", s)
	}
	return as, nil
}

func (as ApprovalStatus) String() string { return string(as) }

// IsTerminal returns true if the status is a terminal state.
func (as ApprovalStatus) IsTerminal() bool {
	return as == StatusApproved || as == StatusDenied || as == StatusExpired
}

// --- ApproverSpec VO ---

// ApproverSpec describes who is required to approve a request.
type ApproverSpec struct {
	Role    string
	ActorID *shared.ActorID
}

// --- ApprovalResolution VO ---

// ApprovalResolution captures the outcome of a resolved approval request.
type ApprovalResolution struct {
	ResolvedBy shared.ActorID
	ResolvedAt shared.Timestamp
	Reason     string
	Status     ApprovalStatus
}

// --- ApprovalRequest aggregate ---

// ApprovalRequest is the aggregate root for approval decisions.
type ApprovalRequest struct {
	id               shared.ApprovalRequestID
	taskID           shared.TaskID
	workflowRunID    shared.WorkflowRunID
	reason           string
	requiredApprover ApproverSpec
	status           ApprovalStatus
	resolution       *ApprovalResolution
	expiresAt        *shared.Timestamp
	createdAt        shared.Timestamp
}

// NewApprovalRequest creates a new ApprovalRequest in pending status.
func NewApprovalRequest(
	id shared.ApprovalRequestID,
	taskID shared.TaskID,
	workflowRunID shared.WorkflowRunID,
	reason string,
	requiredApprover ApproverSpec,
	expiresAt *shared.Timestamp,
	now shared.Timestamp,
) (*ApprovalRequest, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrEmptyReason
	}

	return &ApprovalRequest{
		id:               id,
		taskID:           taskID,
		workflowRunID:    workflowRunID,
		reason:           reason,
		requiredApprover: requiredApprover,
		status:           StatusPending,
		resolution:       nil,
		expiresAt:        expiresAt,
		createdAt:        now,
	}, nil
}

// ReconstructApprovalRequest creates an ApprovalRequest from persisted state without validation.
func ReconstructApprovalRequest(
	id shared.ApprovalRequestID,
	taskID shared.TaskID,
	workflowRunID shared.WorkflowRunID,
	reason string,
	requiredApprover ApproverSpec,
	status ApprovalStatus,
	resolution *ApprovalResolution,
	expiresAt *shared.Timestamp,
	createdAt shared.Timestamp,
) *ApprovalRequest {
	return &ApprovalRequest{
		id:               id,
		taskID:           taskID,
		workflowRunID:    workflowRunID,
		reason:           reason,
		requiredApprover: requiredApprover,
		status:           status,
		resolution:       resolution,
		expiresAt:        expiresAt,
		createdAt:        createdAt,
	}
}

// Approve resolves the request as approved.
func (ar *ApprovalRequest) Approve(resolvedBy shared.ActorID, reason string, now shared.Timestamp) error {
	return ar.resolve(resolvedBy, reason, StatusApproved, now)
}

// Deny resolves the request as denied.
func (ar *ApprovalRequest) Deny(resolvedBy shared.ActorID, reason string, now shared.Timestamp) error {
	return ar.resolve(resolvedBy, reason, StatusDenied, now)
}

// Expire resolves the request as expired (system-initiated).
func (ar *ApprovalRequest) Expire(now shared.Timestamp) error {
	systemActor := shared.ActorID("system")
	return ar.resolve(systemActor, "expired", StatusExpired, now)
}

func (ar *ApprovalRequest) resolve(resolvedBy shared.ActorID, reason string, status ApprovalStatus, now shared.Timestamp) error {
	if ar.status != StatusPending {
		return fmt.Errorf("%w: current status is %q", ErrNotPending, ar.status)
	}
	ar.status = status
	ar.resolution = &ApprovalResolution{
		ResolvedBy: resolvedBy,
		ResolvedAt: now,
		Reason:     reason,
		Status:     status,
	}
	return nil
}

// --- Accessors ---

func (ar *ApprovalRequest) ID() shared.ApprovalRequestID   { return ar.id }
func (ar *ApprovalRequest) TaskID() shared.TaskID           { return ar.taskID }
func (ar *ApprovalRequest) WorkflowRunID() shared.WorkflowRunID { return ar.workflowRunID }
func (ar *ApprovalRequest) Reason() string                  { return ar.reason }
func (ar *ApprovalRequest) RequiredApprover() ApproverSpec   { return ar.requiredApprover }
func (ar *ApprovalRequest) Status() ApprovalStatus          { return ar.status }
func (ar *ApprovalRequest) Resolution() *ApprovalResolution { return ar.resolution }
func (ar *ApprovalRequest) ExpiresAt() *shared.Timestamp    { return ar.expiresAt }
func (ar *ApprovalRequest) CreatedAt() shared.Timestamp     { return ar.createdAt }
