package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type AgentLoopExecutor struct {
	shell Executor
	file  Executor
	git   Executor
}

func NewAgentLoopExecutor(shell Executor, file Executor, git Executor) *AgentLoopExecutor {
	return &AgentLoopExecutor{
		shell: shell,
		file:  file,
		git:   git,
	}
}

func (e *AgentLoopExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	startedAt := spec.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	baseIndex := 0
	if spec.ResumeCheckpoint != nil && spec.ResumeCheckpoint.LastStepIndex > 0 {
		baseIndex = spec.ResumeCheckpoint.LastStepIndex
	}

	planStep := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    baseIndex + 1,
		Kind:     "model",
		Title:    "Agent loop planning",
		Status:   "completed",
		Phase:    "planning",
		Result:   telemetry.ResultSuccess,
		ToolName: "builtin.agent_loop_planner",
		Input: map[string]any{
			"prompt":         spec.Task.Prompt,
			"execution_kind": spec.Task.ExecutionKind,
		},
		OutputSummary: map[string]any{
			"summary": "Planned deterministic tool sequence for agent_loop execution.",
		},
		StartedAt:  startedAt,
		FinishedAt: startedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	if err := upsertTaskStep(spec, planStep); err != nil {
		return nil, err
	}
	planArtifact := newInlineArtifact(spec, planStep.ID, "summary", "agent-plan.txt", "Agent loop plan", "", "Agent loop planned execution sequence.", "ready", startedAt)
	if err := upsertTaskArtifact(spec, planArtifact); err != nil {
		return nil, err
	}

	allSteps := []types.TaskStep{planStep}
	allArtifacts := []types.TaskArtifact{planArtifact}
	nextIndex := baseIndex + 2

	runTool := func(exec Executor, task types.Task) (*ExecutionResult, error) {
		if exec == nil {
			return nil, nil
		}
		result, err := exec.Execute(ctx, ExecutionSpec{
			Task:             task,
			Run:              spec.Run,
			RequestID:        spec.RequestID,
			TraceID:          spec.TraceID,
			RootSpanID:       spec.RootSpanID,
			StartedAt:        time.Now().UTC(),
			ResumeCheckpoint: nil,
			NewID:            spec.NewID,
			UpsertStep:       spec.UpsertStep,
			UpsertArtifact:   spec.UpsertArtifact,
		})
		if err != nil || result == nil {
			return result, err
		}
		for _, step := range result.Steps {
			step.Index = nextIndex
			nextIndex++
			if err := upsertTaskStep(spec, step); err != nil {
				return nil, err
			}
			allSteps = append(allSteps, step)
		}
		allArtifacts = append(allArtifacts, result.Artifacts...)
		return result, nil
	}

	lastError := ""
	otelStatusCode := "ok"
	otelStatusMessage := ""
	status := "completed"
	finalizeFrom := func(result *ExecutionResult) *ExecutionResult {
		if result == nil {
			return &ExecutionResult{
				Status:            status,
				Steps:             allSteps,
				Artifacts:         allArtifacts,
				LastError:         lastError,
				OtelStatusCode:    otelStatusCode,
				OtelStatusMessage: otelStatusMessage,
			}
		}
		return &ExecutionResult{
			Status:            result.Status,
			Steps:             allSteps,
			Artifacts:         allArtifacts,
			LastError:         result.LastError,
			OtelStatusCode:    firstNonEmpty(result.OtelStatusCode, otelStatusCode),
			OtelStatusMessage: firstNonEmpty(result.OtelStatusMessage, otelStatusMessage),
		}
	}

	if spec.Task.ShellCommand != "" {
		task := spec.Task
		task.ExecutionKind = "shell"
		task.FilePath = ""
		task.GitCommand = ""
		result, err := runTool(e.shell, task)
		if err != nil {
			return nil, err
		}
		if result != nil && result.Status != "completed" {
			return finalizeFrom(result), nil
		}
	}
	if spec.Task.GitCommand != "" {
		task := spec.Task
		task.ExecutionKind = "git"
		task.ShellCommand = ""
		task.FilePath = ""
		result, err := runTool(e.git, task)
		if err != nil {
			return nil, err
		}
		if result != nil && result.Status != "completed" {
			return finalizeFrom(result), nil
		}
	}
	if spec.Task.FilePath != "" {
		task := spec.Task
		task.ExecutionKind = "file"
		task.ShellCommand = ""
		task.GitCommand = ""
		result, err := runTool(e.file, task)
		if err != nil {
			return nil, err
		}
		if result != nil && result.Status != "completed" {
			return finalizeFrom(result), nil
		}
	}
	if len(allSteps) == 1 {
		status = "failed"
		lastError = "agent_loop has no runnable tool inputs; set shell_command, git_command, or file_path"
		otelStatusCode = "error"
		otelStatusMessage = lastError
	}

	return finalizeFrom(nil), nil
}
