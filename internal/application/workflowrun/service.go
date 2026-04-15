// Package workflowrun contains use cases for workflow execution, lease management, and lifecycle control.
package workflowrun

import (
	"github.com/russellcxl/agent-governance-core/internal/ports/outbound"
)

// WorkflowRunService coordinates WorkflowRun and ExecutionLease in the application layer.
type WorkflowRunService struct {
	wfRepo           outbound.WorkflowRunRepository
	leaseRepo        outbound.ExecutionLeaseRepository
	taskRepo         outbound.TaskRepository
	routingRepo      outbound.RoutingDecisionRepository
	policyRepo       outbound.PolicyDecisionRepository
	approvalRepo     outbound.ApprovalRequestRepository
	idGen            outbound.IDGenerator
	clock            outbound.Clock
	audit            outbound.AuditRecorder
	notifier         outbound.GovernanceNotifier
	lifecycleMetrics *LifecycleMetrics
}

// NewWorkflowRunService creates a WorkflowRunService with all required dependencies.
func NewWorkflowRunService(
	wfRepo outbound.WorkflowRunRepository,
	leaseRepo outbound.ExecutionLeaseRepository,
	taskRepo outbound.TaskRepository,
	routingRepo outbound.RoutingDecisionRepository,
	policyRepo outbound.PolicyDecisionRepository,
	approvalRepo outbound.ApprovalRequestRepository,
	idGen outbound.IDGenerator,
	clock outbound.Clock,
	audit outbound.AuditRecorder,
	notifier outbound.GovernanceNotifier,
	lifecycleMetrics *LifecycleMetrics,
) *WorkflowRunService {
	return &WorkflowRunService{
		wfRepo:           wfRepo,
		leaseRepo:        leaseRepo,
		taskRepo:         taskRepo,
		routingRepo:      routingRepo,
		policyRepo:       policyRepo,
		approvalRepo:     approvalRepo,
		idGen:            idGen,
		clock:            clock,
		audit:            audit,
		notifier:         notifier,
		lifecycleMetrics: lifecycleMetrics,
	}
}
