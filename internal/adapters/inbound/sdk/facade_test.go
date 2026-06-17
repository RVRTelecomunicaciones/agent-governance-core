package sdk_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/adapters/inbound/sdk"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/application/resilience"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/approval"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/audit"
	escalationdomain "github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/escalation"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/execution"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/policy"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/routing"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/task"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/workflow"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/inbound"
	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/ports/outbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock implementations ---

type mockGovernanceService struct {
	submitTaskFn     func(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error)
	processTaskFn    func(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error)
	routeTaskFn      func(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error)
	evaluatePolicyFn func(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error)
	startWorkflowFn  func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
}

func (m *mockGovernanceService) SubmitTask(ctx context.Context, input inbound.SubmitTaskInput) (*task.Task, error) {
	return m.submitTaskFn(ctx, input)
}
func (m *mockGovernanceService) ProcessTask(ctx context.Context, input inbound.SubmitTaskInput, action string) (*inbound.ProcessTaskResult, error) {
	return m.processTaskFn(ctx, input, action)
}
func (m *mockGovernanceService) RouteTask(ctx context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	return m.routeTaskFn(ctx, taskID)
}
func (m *mockGovernanceService) EvaluatePolicy(ctx context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	return m.evaluatePolicyFn(ctx, taskID, action)
}
func (m *mockGovernanceService) StartWorkflow(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return m.startWorkflowFn(ctx, taskID)
}

type mockWorkflowControl struct {
	killFn    func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	pauseFn   func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	resumeFn  func(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error
	attemptFn func(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error)
}

func (m *mockWorkflowControl) KillWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.killFn(ctx, id, reason, actor)
}
func (m *mockWorkflowControl) PauseWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.pauseFn(ctx, id, reason, actor)
}
func (m *mockWorkflowControl) ResumeWorkflow(ctx context.Context, id shared.WorkflowRunID, reason string, actor shared.ActorID) error {
	return m.resumeFn(ctx, id, reason, actor)
}
func (m *mockWorkflowControl) RegisterAttempt(ctx context.Context, id shared.WorkflowRunID, result execution.AttemptResult) (*workflow.WorkflowRun, error) {
	return m.attemptFn(ctx, id, result)
}

type mockApprovalService struct {
	resolveFn    func(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error)
	getPendingFn func(ctx context.Context) ([]*approval.ApprovalRequest, error)
}

func (m *mockApprovalService) ResolveApproval(ctx context.Context, input inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
	return m.resolveFn(ctx, input)
}
func (m *mockApprovalService) GetPendingApprovals(ctx context.Context) ([]*approval.ApprovalRequest, error) {
	return m.getPendingFn(ctx)
}

type mockQueryService struct {
	getTaskFn     func(ctx context.Context, id shared.TaskID) (*task.Task, error)
	getWorkflowFn func(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error)
	getWfByTaskFn func(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error)
	queryAuditFn  func(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error)
}

func (m *mockQueryService) GetTask(ctx context.Context, id shared.TaskID) (*task.Task, error) {
	return m.getTaskFn(ctx, id)
}
func (m *mockQueryService) GetWorkflowStatus(ctx context.Context, id shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
	return m.getWorkflowFn(ctx, id)
}
func (m *mockQueryService) GetWorkflowByTask(ctx context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	return m.getWfByTaskFn(ctx, taskID)
}
func (m *mockQueryService) QueryAuditTrail(ctx context.Context, filter outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
	return m.queryAuditFn(ctx, filter)
}

func (m *mockQueryService) ListWorkflows(_ context.Context, _ outbound.WorkflowListFilter) ([]*workflow.WorkflowRun, int, error) {
	return nil, 0, nil
}

func (m *mockQueryService) ListBreakers(_ context.Context, _ resilience.BreakerFilter) ([]resilience.BreakerSnapshot, error) {
	return nil, nil
}

func (m *mockQueryService) GetBreakerState(_ context.Context, _, _ string) (*resilience.BreakerSnapshot, error) {
	return nil, nil
}

type mockEscalationPort struct{}

func (m *mockEscalationPort) TriggerEscalation(_ context.Context, _ shared.TaskID, _ escalationdomain.EscalationCondition, _ escalationdomain.EscalationTarget) (*escalationdomain.EscalationTrigger, error) {
	return nil, nil
}

// --- Tests ---

func TestFacade_DelegatesToGovernanceService(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	taskID := shared.TaskID("01KP7XXH5Z4F9W4P14SNKPF787")
	fakeTask, _ := task.NewTask(taskID, nil, task.TypeDevelopment, "test", task.ScopeFile, shared.PriorityNormal, shared.RiskLevelLow, nil, now)

	gov := &mockGovernanceService{
		submitTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput) (*task.Task, error) {
			return fakeTask, nil
		},
		processTaskFn: func(_ context.Context, _ inbound.SubmitTaskInput, _ string) (*inbound.ProcessTaskResult, error) {
			return &inbound.ProcessTaskResult{Task: fakeTask}, nil
		},
		routeTaskFn: func(_ context.Context, _ shared.TaskID) (*routing.RoutingDecision, error) {
			return nil, errors.New("not found")
		},
		evaluatePolicyFn: func(_ context.Context, _ shared.TaskID, _ string) (*policy.PolicyDecision, error) {
			return nil, nil
		},
		startWorkflowFn: func(_ context.Context, _ shared.TaskID) (*workflow.WorkflowRun, error) {
			return nil, nil
		},
	}

	facade := sdk.NewGovernanceFacade(gov, &mockWorkflowControl{}, &mockApprovalService{}, &mockQueryService{}, &mockEscalationPort{})

	t.Run("SubmitTask delegates", func(t *testing.T) {
		result, err := facade.SubmitTask(context.Background(), inbound.SubmitTaskInput{})
		require.NoError(t, err)
		assert.Equal(t, fakeTask, result)
	})

	t.Run("ProcessTask delegates", func(t *testing.T) {
		result, err := facade.ProcessTask(context.Background(), inbound.SubmitTaskInput{}, "execute")
		require.NoError(t, err)
		assert.Equal(t, fakeTask, result.Task)
	})

	t.Run("RouteTask delegates and passes errors", func(t *testing.T) {
		_, err := facade.RouteTask(context.Background(), taskID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestFacade_DelegatesToWorkflowControl(t *testing.T) {
	var called bool
	ctrl := &mockWorkflowControl{
		killFn: func(_ context.Context, _ shared.WorkflowRunID, _ string, _ shared.ActorID) error {
			called = true
			return nil
		},
		pauseFn: func(_ context.Context, _ shared.WorkflowRunID, _ string, _ shared.ActorID) error {
			return nil
		},
		resumeFn: func(_ context.Context, _ shared.WorkflowRunID, _ string, _ shared.ActorID) error {
			return nil
		},
		attemptFn: func(_ context.Context, _ shared.WorkflowRunID, _ execution.AttemptResult) (*workflow.WorkflowRun, error) {
			return nil, nil
		},
	}

	facade := sdk.NewGovernanceFacade(&mockGovernanceService{}, ctrl, &mockApprovalService{}, &mockQueryService{}, &mockEscalationPort{})

	err := facade.KillWorkflow(context.Background(), shared.WorkflowRunID("01KP7XXH5Z4F9W4P14SNKPF787"), "test", shared.ActorID("admin"))
	require.NoError(t, err)
	assert.True(t, called)
}

func TestFacade_DelegatesToApprovalService(t *testing.T) {
	var called bool
	approvals := &mockApprovalService{
		resolveFn: func(_ context.Context, _ inbound.ResolveApprovalInput) (*approval.ApprovalRequest, error) {
			return nil, nil
		},
		getPendingFn: func(_ context.Context) ([]*approval.ApprovalRequest, error) {
			called = true
			return nil, nil
		},
	}

	facade := sdk.NewGovernanceFacade(&mockGovernanceService{}, &mockWorkflowControl{}, approvals, &mockQueryService{}, &mockEscalationPort{})

	_, err := facade.GetPendingApprovals(context.Background())
	require.NoError(t, err)
	assert.True(t, called)
}

func TestFacade_DelegatesToQueryService(t *testing.T) {
	now := shared.MustTimestamp(time.Now())
	taskID := shared.TaskID("01KP7XXH5Z4F9W4P14SNKPF787")
	fakeTask, _ := task.NewTask(taskID, nil, task.TypeDevelopment, "test", task.ScopeFile, shared.PriorityNormal, shared.RiskLevelLow, nil, now)

	queries := &mockQueryService{
		getTaskFn: func(_ context.Context, _ shared.TaskID) (*task.Task, error) {
			return fakeTask, nil
		},
		getWorkflowFn: func(_ context.Context, _ shared.WorkflowRunID) (*workflow.WorkflowRun, error) {
			return nil, nil
		},
		getWfByTaskFn: func(_ context.Context, _ shared.TaskID) (*workflow.WorkflowRun, error) {
			return nil, nil
		},
		queryAuditFn: func(_ context.Context, _ outbound.AuditFilter) ([]*audit.AuditEntry, int, error) {
			return nil, 42, nil
		},
	}

	facade := sdk.NewGovernanceFacade(&mockGovernanceService{}, &mockWorkflowControl{}, &mockApprovalService{}, queries, &mockEscalationPort{})

	t.Run("GetTask delegates", func(t *testing.T) {
		result, err := facade.GetTask(context.Background(), taskID)
		require.NoError(t, err)
		assert.Equal(t, fakeTask, result)
	})

	t.Run("QueryAuditTrail delegates", func(t *testing.T) {
		_, count, err := facade.QueryAuditTrail(context.Background(), outbound.AuditFilter{})
		require.NoError(t, err)
		assert.Equal(t, 42, count)
	})
}
