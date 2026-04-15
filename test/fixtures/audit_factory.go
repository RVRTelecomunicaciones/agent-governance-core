package fixtures

import (
	"github.com/russellcxl/agent-governance-core/internal/domain/audit"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// AuditEntryOption configures a test audit entry.
type AuditEntryOption func(*auditEntryConfig)

type auditEntryConfig struct {
	id            shared.AuditEntryID
	actor         shared.ActorID
	action        string
	outcome       string
	ctx           audit.AuditContext
	taskID        *shared.TaskID
	workflowRunID *shared.WorkflowRunID
}

func defaultAuditEntryConfig() auditEntryConfig {
	id, _ := shared.NewAuditEntryID(newULID())
	actor, _ := shared.NewActorID("test-system")
	return auditEntryConfig{
		id:      id,
		actor:   actor,
		action:  "test_action",
		outcome: "success",
		ctx:     audit.NewAuditContext(),
	}
}

// WithAuditEntryID sets the audit entry ID.
func WithAuditEntryID(id shared.AuditEntryID) AuditEntryOption {
	return func(c *auditEntryConfig) { c.id = id }
}

// WithAuditActor sets the actor.
func WithAuditActor(actor shared.ActorID) AuditEntryOption {
	return func(c *auditEntryConfig) { c.actor = actor }
}

// WithAuditAction sets the action.
func WithAuditAction(action string) AuditEntryOption {
	return func(c *auditEntryConfig) { c.action = action }
}

// WithAuditOutcome sets the outcome.
func WithAuditOutcome(outcome string) AuditEntryOption {
	return func(c *auditEntryConfig) { c.outcome = outcome }
}

// WithAuditTaskID sets the optional task ID reference.
func WithAuditTaskID(id shared.TaskID) AuditEntryOption {
	return func(c *auditEntryConfig) { c.taskID = &id }
}

// WithAuditWorkflowRunID sets the optional workflow run ID reference.
func WithAuditWorkflowRunID(id shared.WorkflowRunID) AuditEntryOption {
	return func(c *auditEntryConfig) { c.workflowRunID = &id }
}

// WithAuditContext sets the audit context.
func WithAuditContext(ctx audit.AuditContext) AuditEntryOption {
	return func(c *auditEntryConfig) { c.ctx = ctx }
}

// NewTestAuditEntry creates a valid AuditEntry. Panics on error (test-only).
func NewTestAuditEntry(opts ...AuditEntryOption) *audit.AuditEntry {
	cfg := defaultAuditEntryConfig()
	for _, o := range opts {
		o(&cfg)
	}

	entry, err := audit.NewAuditEntry(
		cfg.id,
		cfg.actor,
		cfg.action,
		cfg.outcome,
		cfg.ctx,
		now(),
	)
	if err != nil {
		panic("fixtures.NewTestAuditEntry: " + err.Error())
	}

	if cfg.taskID != nil {
		entry.WithTaskID(*cfg.taskID)
	}
	if cfg.workflowRunID != nil {
		entry.WithWorkflowRunID(*cfg.workflowRunID)
	}

	return entry
}
