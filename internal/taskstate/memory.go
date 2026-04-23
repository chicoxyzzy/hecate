package taskstate

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

type MemoryStore struct {
	mu        sync.Mutex
	tasks     map[string]types.Task
	runs      map[string]types.TaskRun
	steps     map[string]types.TaskStep
	approvals map[string]types.TaskApproval
	artifacts map[string]types.TaskArtifact
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tasks:     make(map[string]types.Task),
		runs:      make(map[string]types.TaskRun),
		steps:     make(map[string]types.TaskStep),
		approvals: make(map[string]types.TaskApproval),
		artifacts: make(map[string]types.TaskArtifact),
	}
}

func (s *MemoryStore) Backend() string {
	return "memory"
}

func (s *MemoryStore) CreateTask(_ context.Context, task types.Task) (types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ID == "" {
		return types.Task{}, fmt.Errorf("task id is required")
	}
	now := time.Now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	s.tasks[task.ID] = task
	return task, nil
}

func (s *MemoryStore) GetTask(_ context.Context, id string) (types.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return types.Task{}, false, nil
	}
	return task, true, nil
}

func (s *MemoryStore) ListTasks(_ context.Context, filter TaskFilter) ([]types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		if filter.Tenant != "" && task.Tenant != filter.Tenant {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *MemoryStore) UpdateTask(_ context.Context, task types.Task) (types.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[task.ID]; !ok {
		return types.Task{}, fmt.Errorf("task %q not found", task.ID)
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = time.Now().UTC()
	}
	s.tasks[task.ID] = task
	return task, nil
}

func (s *MemoryStore) CreateRun(_ context.Context, run types.TaskRun) (types.TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.ID == "" {
		return types.TaskRun{}, fmt.Errorf("run id is required")
	}
	s.runs[run.ID] = run
	return run, nil
}

func (s *MemoryStore) GetRun(_ context.Context, taskID, runID string) (types.TaskRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok || (taskID != "" && run.TaskID != taskID) {
		return types.TaskRun{}, false, nil
	}
	return run, true, nil
}

func (s *MemoryStore) ListRuns(_ context.Context, taskID string) ([]types.TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.TaskRun, 0)
	for _, run := range s.runs {
		if taskID != "" && run.TaskID != taskID {
			continue
		}
		items = append(items, run)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Number == items[j].Number {
			return items[i].ID < items[j].ID
		}
		return items[i].Number > items[j].Number
	})
	return items, nil
}

func (s *MemoryStore) UpdateRun(_ context.Context, run types.TaskRun) (types.TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return types.TaskRun{}, fmt.Errorf("run %q not found", run.ID)
	}
	s.runs[run.ID] = run
	return run, nil
}

func (s *MemoryStore) AppendStep(_ context.Context, step types.TaskStep) (types.TaskStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if step.ID == "" {
		return types.TaskStep{}, fmt.Errorf("step id is required")
	}
	s.steps[step.ID] = step
	return step, nil
}

func (s *MemoryStore) GetStep(_ context.Context, runID, stepID string) (types.TaskStep, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	step, ok := s.steps[stepID]
	if !ok || (runID != "" && step.RunID != runID) {
		return types.TaskStep{}, false, nil
	}
	return step, true, nil
}

func (s *MemoryStore) ListSteps(_ context.Context, runID string) ([]types.TaskStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.TaskStep, 0)
	for _, step := range s.steps {
		if runID != "" && step.RunID != runID {
			continue
		}
		items = append(items, step)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Index == items[j].Index {
			return items[i].ID < items[j].ID
		}
		return items[i].Index < items[j].Index
	})
	return items, nil
}

func (s *MemoryStore) UpdateStep(_ context.Context, step types.TaskStep) (types.TaskStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.steps[step.ID]; !ok {
		return types.TaskStep{}, fmt.Errorf("step %q not found", step.ID)
	}
	s.steps[step.ID] = step
	return step, nil
}

func (s *MemoryStore) CreateApproval(_ context.Context, approval types.TaskApproval) (types.TaskApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if approval.ID == "" {
		return types.TaskApproval{}, fmt.Errorf("approval id is required")
	}
	s.approvals[approval.ID] = approval
	return approval, nil
}

func (s *MemoryStore) GetApproval(_ context.Context, taskID, approvalID string) (types.TaskApproval, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	approval, ok := s.approvals[approvalID]
	if !ok || (taskID != "" && approval.TaskID != taskID) {
		return types.TaskApproval{}, false, nil
	}
	return approval, true, nil
}

func (s *MemoryStore) ListApprovals(_ context.Context, taskID string) ([]types.TaskApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.TaskApproval, 0)
	for _, approval := range s.approvals {
		if taskID != "" && approval.TaskID != taskID {
			continue
		}
		items = append(items, approval)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryStore) UpdateApproval(_ context.Context, approval types.TaskApproval) (types.TaskApproval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.approvals[approval.ID]; !ok {
		return types.TaskApproval{}, fmt.Errorf("approval %q not found", approval.ID)
	}
	s.approvals[approval.ID] = approval
	return approval, nil
}

func (s *MemoryStore) CreateArtifact(_ context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if artifact.ID == "" {
		return types.TaskArtifact{}, fmt.Errorf("artifact id is required")
	}
	s.artifacts[artifact.ID] = artifact
	return artifact, nil
}

func (s *MemoryStore) GetArtifact(_ context.Context, taskID, artifactID string) (types.TaskArtifact, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	artifact, ok := s.artifacts[artifactID]
	if !ok || (taskID != "" && artifact.TaskID != taskID) {
		return types.TaskArtifact{}, false, nil
	}
	return artifact, true, nil
}

func (s *MemoryStore) ListArtifacts(_ context.Context, filter ArtifactFilter) ([]types.TaskArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.TaskArtifact, 0)
	for _, artifact := range s.artifacts {
		if filter.TaskID != "" && artifact.TaskID != filter.TaskID {
			continue
		}
		if filter.RunID != "" && artifact.RunID != filter.RunID {
			continue
		}
		if filter.StepID != "" && artifact.StepID != filter.StepID {
			continue
		}
		if filter.Kind != "" && artifact.Kind != filter.Kind {
			continue
		}
		items = append(items, artifact)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *MemoryStore) UpdateArtifact(_ context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.artifacts[artifact.ID]; !ok {
		return types.TaskArtifact{}, fmt.Errorf("artifact %q not found", artifact.ID)
	}
	s.artifacts[artifact.ID] = artifact
	return artifact, nil
}
