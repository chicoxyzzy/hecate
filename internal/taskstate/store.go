package taskstate

import (
	"context"

	"github.com/hecate/agent-runtime/pkg/types"
)

type TaskFilter struct {
	Tenant string
	Status string
	Limit  int
}

type ArtifactFilter struct {
	TaskID string
	RunID  string
	StepID string
	Kind   string
	Limit  int
}

type Store interface {
	Backend() string
	CreateTask(ctx context.Context, task types.Task) (types.Task, error)
	GetTask(ctx context.Context, id string) (types.Task, bool, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]types.Task, error)
	UpdateTask(ctx context.Context, task types.Task) (types.Task, error)

	CreateRun(ctx context.Context, run types.TaskRun) (types.TaskRun, error)
	GetRun(ctx context.Context, taskID, runID string) (types.TaskRun, bool, error)
	ListRuns(ctx context.Context, taskID string) ([]types.TaskRun, error)
	UpdateRun(ctx context.Context, run types.TaskRun) (types.TaskRun, error)

	AppendStep(ctx context.Context, step types.TaskStep) (types.TaskStep, error)
	GetStep(ctx context.Context, runID, stepID string) (types.TaskStep, bool, error)
	ListSteps(ctx context.Context, runID string) ([]types.TaskStep, error)
	UpdateStep(ctx context.Context, step types.TaskStep) (types.TaskStep, error)

	CreateApproval(ctx context.Context, approval types.TaskApproval) (types.TaskApproval, error)
	GetApproval(ctx context.Context, taskID, approvalID string) (types.TaskApproval, bool, error)
	ListApprovals(ctx context.Context, taskID string) ([]types.TaskApproval, error)
	UpdateApproval(ctx context.Context, approval types.TaskApproval) (types.TaskApproval, error)

	CreateArtifact(ctx context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error)
	GetArtifact(ctx context.Context, taskID, artifactID string) (types.TaskArtifact, bool, error)
	ListArtifacts(ctx context.Context, filter ArtifactFilter) ([]types.TaskArtifact, error)
	UpdateArtifact(ctx context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error)
}
