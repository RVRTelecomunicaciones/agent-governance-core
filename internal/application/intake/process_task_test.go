package intake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/russellcxl/agent-governance-core/internal/domain/policy"
	"github.com/russellcxl/agent-governance-core/internal/domain/routing"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
	"github.com/russellcxl/agent-governance-core/internal/domain/task"
	"github.com/russellcxl/agent-governance-core/internal/domain/workflow"
	"github.com/russellcxl/agent-governance-core/internal/ports/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Process task mocks ---

type mockRouter struct {
	called bool
	taskID shared.TaskID
	result *routing.RoutingDecision
	err    error
}

func (m *mockRouter) RouteTask(_ context.Context, taskID shared.TaskID) (*routing.RoutingDecision, error) {
	m.called = true
	m.taskID = taskID
	return m.result, m.err
}

type mockPolicyEvaluator struct {
	called bool
	taskID shared.TaskID
	action string
	result *policy.PolicyDecision
	err    error
}

func (m *mockPolicyEvaluator) EvaluatePolicy(_ context.Context, taskID shared.TaskID, action string) (*policy.PolicyDecision, error) {
	m.called = true
	m.taskID = taskID
	m.action = action
	return m.result, m.err
}

type mockWorkflowStarter struct {
	called bool
	taskID shared.TaskID
	result *workflow.WorkflowRun
	err    error
}

func (m *mockWorkflowStarter) StartWorkflow(_ context.Context, taskID shared.TaskID) (*workflow.WorkflowRun, error) {
	m.called = true
	m.taskID = taskID
	return m.result, m.err
}

// --- Helpers ---

var (
	testNow   = shared.MustTimestamp(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	testInput = inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "process task test",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	}
)

const processTestTaskID = "01JSGW0000000000000000000A"

func newProcessTestService() (*ProcessTaskService, *mockRouter, *mockPolicyEvaluator, *mockWorkflowStarter) {
	submitSvc := NewSubmitTaskService(
		&mockTaskRepo{},
		&mockIDGen{taskID: shared.TaskID(processTestTaskID)},
		&mockClock{now: testNow},
		&mockAuditRecorder{},
		nil,
	)

	router := &mockRouter{
		result: routing.Reconstruct(
			shared.RoutingDecisionID("01JSGW0000000000000000000B"),
			shared.TaskID(processTestTaskID),
			[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 1.0, FactorScores: map[string]float64{"base": 1.0}}},
			routing.StrategyDirect,
			routing.RoleImplementer,
			"direct strategy selected",
			nil,
			testNow,
		),
	}

	policyEval := &mockPolicyEvaluator{
		result: mustPolicyDecision(),
	}

	wfStarter := &mockWorkflowStarter{
		result: workflow.NewWorkflowRun(
			shared.WorkflowRunID("01JSGW0000000000000000000D"),
			shared.TaskID(processTestTaskID),
			testNow,
		),
	}

	svc := NewProcessTaskService(submitSvc, router, policyEval, wfStarter)
	return svc, router, policyEval, wfStarter
}

func mustPolicyDecision() *policy.PolicyDecision {
	pd, err := policy.NewPolicyDecision(
		shared.PolicyDecisionID("01JSGW0000000000000000000C"),
		shared.TaskID(processTestTaskID),
		"execute",
		policy.OutcomeAllow,
		nil,
		nil,
		[]policy.RuleEvaluation{{RuleID: "test-rule", Passed: true, Reason: "allowed"}},
		"allowed by default",
		testNow,
	)
	if err != nil {
		panic(err)
	}
	return pd
}

// --- Tests ---

func TestProcessTask_AllStepsInOrder(t *testing.T) {
	svc, router, policyEval, wfStarter := newProcessTestService()

	result, err := svc.ProcessTask(context.Background(), testInput, "execute")

	require.NoError(t, err)
	require.NotNil(t, result)

	// All steps called
	assert.True(t, router.called)
	assert.True(t, policyEval.called)
	assert.True(t, wfStarter.called)
}

func TestProcessTask_ReturnsAllIntermediateResults(t *testing.T) {
	svc, _, _, _ := newProcessTestService()

	result, err := svc.ProcessTask(context.Background(), testInput, "execute")

	require.NoError(t, err)
	assert.NotNil(t, result.Task)
	assert.NotNil(t, result.RoutingDecision)
	assert.NotNil(t, result.PolicyDecision)
	assert.NotNil(t, result.WorkflowRun)
}

func TestProcessTask_FailsIfSubmitTaskFails(t *testing.T) {
	submitSvc := NewSubmitTaskService(
		&mockTaskRepo{},
		&mockIDGen{taskID: shared.TaskID(processTestTaskID)},
		&mockClock{now: testNow},
		&mockAuditRecorder{},
		nil,
	)
	router := &mockRouter{}
	policyEval := &mockPolicyEvaluator{}
	wfStarter := &mockWorkflowStarter{}

	svc := NewProcessTaskService(submitSvc, router, policyEval, wfStarter)

	// Empty title triggers submit failure
	_, err := svc.ProcessTask(context.Background(), inbound.SubmitTaskInput{
		Type:     task.TypeDevelopment,
		Title:    "",
		Scope:    task.ScopeFile,
		Priority: shared.PriorityNormal,
	}, "execute")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "submit task")
	assert.False(t, router.called)
	assert.False(t, policyEval.called)
	assert.False(t, wfStarter.called)
}

func TestProcessTask_FailsIfRouteTaskFails(t *testing.T) {
	submitSvc := NewSubmitTaskService(
		&mockTaskRepo{},
		&mockIDGen{taskID: shared.TaskID(processTestTaskID)},
		&mockClock{now: testNow},
		&mockAuditRecorder{},
		nil,
	)
	router := &mockRouter{err: errors.New("routing unavailable")}
	policyEval := &mockPolicyEvaluator{}
	wfStarter := &mockWorkflowStarter{}

	svc := NewProcessTaskService(submitSvc, router, policyEval, wfStarter)

	_, err := svc.ProcessTask(context.Background(), testInput, "execute")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "route task")
	assert.True(t, router.called)
	assert.False(t, policyEval.called)
	assert.False(t, wfStarter.called)
}

func TestProcessTask_FailsIfEvaluatePolicyFails(t *testing.T) {
	submitSvc := NewSubmitTaskService(
		&mockTaskRepo{},
		&mockIDGen{taskID: shared.TaskID(processTestTaskID)},
		&mockClock{now: testNow},
		&mockAuditRecorder{},
		nil,
	)
	router := &mockRouter{
		result: routing.Reconstruct(
			shared.RoutingDecisionID("01JSGW0000000000000000000B"),
			shared.TaskID(processTestTaskID),
			[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 1.0, FactorScores: map[string]float64{"base": 1.0}}},
			routing.StrategyDirect,
			routing.RoleImplementer,
			"direct",
			nil,
			testNow,
		),
	}
	policyEval := &mockPolicyEvaluator{err: errors.New("policy engine down")}
	wfStarter := &mockWorkflowStarter{}

	svc := NewProcessTaskService(submitSvc, router, policyEval, wfStarter)

	_, err := svc.ProcessTask(context.Background(), testInput, "execute")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "evaluate policy")
	assert.True(t, router.called)
	assert.True(t, policyEval.called)
	assert.False(t, wfStarter.called)
}

func TestProcessTask_FailsIfStartWorkflowFails(t *testing.T) {
	submitSvc := NewSubmitTaskService(
		&mockTaskRepo{},
		&mockIDGen{taskID: shared.TaskID(processTestTaskID)},
		&mockClock{now: testNow},
		&mockAuditRecorder{},
		nil,
	)
	router := &mockRouter{
		result: routing.Reconstruct(
			shared.RoutingDecisionID("01JSGW0000000000000000000B"),
			shared.TaskID(processTestTaskID),
			[]routing.StrategyEvaluation{{Strategy: routing.StrategyDirect, Score: 1.0, FactorScores: map[string]float64{"base": 1.0}}},
			routing.StrategyDirect,
			routing.RoleImplementer,
			"direct",
			nil,
			testNow,
		),
	}
	policyEval := &mockPolicyEvaluator{result: mustPolicyDecision()}
	wfStarter := &mockWorkflowStarter{err: errors.New("workflow engine down")}

	svc := NewProcessTaskService(submitSvc, router, policyEval, wfStarter)

	_, err := svc.ProcessTask(context.Background(), testInput, "execute")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start workflow")
	assert.True(t, router.called)
	assert.True(t, policyEval.called)
	assert.True(t, wfStarter.called)
}
