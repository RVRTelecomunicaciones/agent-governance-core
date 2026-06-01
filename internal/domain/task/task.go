package task

import (
	"errors"
	"fmt"
	"strings"

	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

// Errors for the task aggregate.
var (
	ErrEmptyTitle        = errors.New("task title must not be empty")
	ErrInvalidTransition = errors.New("invalid status transition")
)

// --- TaskType enum ---

// TaskType classifies what kind of work a task represents.
type TaskType string

const (
	TypeDevelopment TaskType = "development"
	TypeReview      TaskType = "review"
	TypeResearch    TaskType = "research"
	TypeRefactor    TaskType = "refactor"
	TypeBugfix      TaskType = "bugfix"
	TypeDeployment  TaskType = "deployment"
)

var validTaskTypes = map[TaskType]bool{
	TypeDevelopment: true,
	TypeReview:      true,
	TypeResearch:    true,
	TypeRefactor:    true,
	TypeBugfix:      true,
	TypeDeployment:  true,
}

func NewTaskType(s string) (TaskType, error) {
	tt := TaskType(s)
	if !validTaskTypes[tt] {
		return "", fmt.Errorf("invalid task type: %q", s)
	}
	return tt, nil
}

func (tt TaskType) String() string { return string(tt) }

// --- TaskScope enum ---

// TaskScope indicates the blast radius of a task.
type TaskScope string

const (
	ScopeFile   TaskScope = "file"
	ScopeModule TaskScope = "module"
	ScopeRepo   TaskScope = "repo"
	ScopeSystem TaskScope = "system"
)

var validTaskScopes = map[TaskScope]bool{
	ScopeFile:   true,
	ScopeModule: true,
	ScopeRepo:   true,
	ScopeSystem: true,
}

func NewTaskScope(s string) (TaskScope, error) {
	ts := TaskScope(s)
	if !validTaskScopes[ts] {
		return "", fmt.Errorf("invalid task scope: %q", s)
	}
	return ts, nil
}

func (ts TaskScope) String() string { return string(ts) }

// --- TaskStatus enum ---

// TaskStatus tracks the intake lifecycle of a task.
// This is NOT the workflow execution status — WorkflowStatus (in a different
// bounded context) is the source of truth for execution state.
type TaskStatus string

const (
	StatusCreated    TaskStatus = "created"
	StatusAccepted   TaskStatus = "accepted"
	StatusInProgress TaskStatus = "in_progress"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
	StatusCancelled  TaskStatus = "cancelled"
)

var validTaskStatuses = map[TaskStatus]bool{
	StatusCreated:    true,
	StatusAccepted:   true,
	StatusInProgress: true,
	StatusCompleted:  true,
	StatusFailed:     true,
	StatusCancelled:  true,
}

func NewTaskStatus(s string) (TaskStatus, error) {
	ts := TaskStatus(s)
	if !validTaskStatuses[ts] {
		return "", fmt.Errorf("invalid task status: %q", s)
	}
	return ts, nil
}

func (ts TaskStatus) String() string { return string(ts) }

// validTransitions defines the allowed status transitions.
// Terminal states (completed, failed, cancelled) have no outgoing transitions.
var validTransitions = map[TaskStatus]map[TaskStatus]bool{
	StatusCreated:    {StatusAccepted: true, StatusCancelled: true},
	StatusAccepted:   {StatusInProgress: true, StatusCancelled: true},
	StatusInProgress: {StatusCompleted: true, StatusFailed: true, StatusCancelled: true},
}

// --- TaskMetadata ---

// TaskMetadata holds arbitrary key-value data associated with a task.
// It is immutable after task creation.
type TaskMetadata map[string]any

// --- Task aggregate ---

// Task is the aggregate root for the governance pipeline entry point.
// Without a Task, nothing else in the system exists.
type Task struct {
	id           shared.TaskID
	parentTaskID *shared.TaskID
	taskType     TaskType
	title        string
	scope        TaskScope
	priority     shared.Priority
	riskLevel    shared.RiskLevel
	status       TaskStatus
	metadata     TaskMetadata
	createdAt    shared.Timestamp
	updatedAt    shared.Timestamp
}

// NewTask creates a new Task in the "created" status with full validation.
func NewTask(
	id shared.TaskID,
	parentTaskID *shared.TaskID,
	taskType TaskType,
	title string,
	scope TaskScope,
	priority shared.Priority,
	riskLevel shared.RiskLevel,
	metadata TaskMetadata,
	now shared.Timestamp,
) (*Task, error) {
	if strings.TrimSpace(title) == "" {
		return nil, ErrEmptyTitle
	}

	meta := make(TaskMetadata, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}

	return &Task{
		id:           id,
		parentTaskID: parentTaskID,
		taskType:     taskType,
		title:        title,
		scope:        scope,
		priority:     priority,
		riskLevel:    riskLevel,
		status:       StatusCreated,
		metadata:     meta,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// Reconstruct creates a Task from persisted state without validation.
// Intended for repository hydration only.
func Reconstruct(
	id shared.TaskID,
	parentTaskID *shared.TaskID,
	taskType TaskType,
	title string,
	scope TaskScope,
	priority shared.Priority,
	riskLevel shared.RiskLevel,
	status TaskStatus,
	metadata TaskMetadata,
	createdAt shared.Timestamp,
	updatedAt shared.Timestamp,
) *Task {
	meta := make(TaskMetadata, len(metadata))
	for k, v := range metadata {
		meta[k] = v
	}

	return &Task{
		id:           id,
		parentTaskID: parentTaskID,
		taskType:     taskType,
		title:        title,
		scope:        scope,
		priority:     priority,
		riskLevel:    riskLevel,
		status:       status,
		metadata:     meta,
		createdAt:    createdAt,
		updatedAt:    updatedAt,
	}
}

// --- Transition methods ---

// Accept transitions the task from created to accepted.
func (t *Task) Accept(now shared.Timestamp) error {
	return t.transitionTo(StatusAccepted, now)
}

// Start transitions the task from accepted to in_progress.
func (t *Task) Start(now shared.Timestamp) error {
	return t.transitionTo(StatusInProgress, now)
}

// Complete transitions the task from in_progress to completed.
func (t *Task) Complete(now shared.Timestamp) error {
	return t.transitionTo(StatusCompleted, now)
}

// Fail transitions the task from in_progress to failed.
func (t *Task) Fail(now shared.Timestamp) error {
	return t.transitionTo(StatusFailed, now)
}

// Cancel transitions the task to cancelled from any non-terminal state.
func (t *Task) Cancel(now shared.Timestamp) error {
	return t.transitionTo(StatusCancelled, now)
}

func (t *Task) transitionTo(target TaskStatus, now shared.Timestamp) error {
	allowed, ok := validTransitions[t.status]
	if !ok || !allowed[target] {
		return fmt.Errorf("%w: cannot transition from %q to %q", ErrInvalidTransition, t.status, target)
	}
	t.status = target
	t.updatedAt = now
	return nil
}

// --- Accessors ---

func (t *Task) ID() shared.TaskID            { return t.id }
func (t *Task) ParentTaskID() *shared.TaskID { return t.parentTaskID }
func (t *Task) Type() TaskType               { return t.taskType }
func (t *Task) Title() string                { return t.title }
func (t *Task) Scope() TaskScope             { return t.scope }
func (t *Task) Priority() shared.Priority    { return t.priority }
func (t *Task) RiskLevel() shared.RiskLevel  { return t.riskLevel }
func (t *Task) Status() TaskStatus           { return t.status }
func (t *Task) CreatedAt() shared.Timestamp  { return t.createdAt }
func (t *Task) UpdatedAt() shared.Timestamp  { return t.updatedAt }

// Metadata returns a defensive copy of the task metadata.
func (t *Task) Metadata() TaskMetadata {
	cp := make(TaskMetadata, len(t.metadata))
	for k, v := range t.metadata {
		cp[k] = v
	}
	return cp
}
