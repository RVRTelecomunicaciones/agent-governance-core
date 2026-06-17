package audit

import (
	"errors"
	"strings"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
)

// Errors for the audit aggregate.
var (
	ErrEmptyAction  = errors.New("audit action must not be empty")
	ErrEmptyOutcome = errors.New("audit outcome must not be empty")
)

// AuditEntry is an append-only aggregate — never edited, never deleted.
type AuditEntry struct {
	id            shared.AuditEntryID
	taskID        *shared.TaskID
	workflowRunID *shared.WorkflowRunID
	actor         shared.ActorID
	action        string
	outcome       string
	context       AuditContext
	createdAt     shared.Timestamp
}

// NewAuditEntry creates a new AuditEntry with required field validation.
func NewAuditEntry(
	id shared.AuditEntryID,
	actor shared.ActorID,
	action string,
	outcome string,
	ctx AuditContext,
	now shared.Timestamp,
) (*AuditEntry, error) {
	if strings.TrimSpace(action) == "" {
		return nil, ErrEmptyAction
	}
	if strings.TrimSpace(outcome) == "" {
		return nil, ErrEmptyOutcome
	}

	return &AuditEntry{
		id:        id,
		actor:     actor,
		action:    action,
		outcome:   outcome,
		context:   ctx,
		createdAt: now,
	}, nil
}

// ReconstructAuditEntry creates an AuditEntry from persisted state without validation.
func ReconstructAuditEntry(
	id shared.AuditEntryID,
	taskID *shared.TaskID,
	workflowRunID *shared.WorkflowRunID,
	actor shared.ActorID,
	action string,
	outcome string,
	ctx AuditContext,
	createdAt shared.Timestamp,
) *AuditEntry {
	return &AuditEntry{
		id:            id,
		taskID:        taskID,
		workflowRunID: workflowRunID,
		actor:         actor,
		action:        action,
		outcome:       outcome,
		context:       ctx,
		createdAt:     createdAt,
	}
}

// WithTaskID sets the optional task reference.
func (ae *AuditEntry) WithTaskID(id shared.TaskID) *AuditEntry {
	ae.taskID = &id
	return ae
}

// WithWorkflowRunID sets the optional workflow run reference.
func (ae *AuditEntry) WithWorkflowRunID(id shared.WorkflowRunID) *AuditEntry {
	ae.workflowRunID = &id
	return ae
}

// --- Accessors ---

func (ae *AuditEntry) ID() shared.AuditEntryID              { return ae.id }
func (ae *AuditEntry) TaskID() *shared.TaskID               { return ae.taskID }
func (ae *AuditEntry) WorkflowRunID() *shared.WorkflowRunID { return ae.workflowRunID }
func (ae *AuditEntry) Actor() shared.ActorID                { return ae.actor }
func (ae *AuditEntry) Action() string                       { return ae.action }
func (ae *AuditEntry) Outcome() string                      { return ae.outcome }
func (ae *AuditEntry) Context() AuditContext                { return ae.context }
func (ae *AuditEntry) CreatedAt() shared.Timestamp          { return ae.createdAt }
