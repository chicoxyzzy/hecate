package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/sandbox"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Executor interface {
	Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error)
}

type ExecutionSpec struct {
	Task       types.Task
	Run        types.TaskRun
	RequestID  string
	TraceID    string
	RootSpanID string
	StartedAt  time.Time
	NewID      func(prefix string) string
}

type ExecutionResult struct {
	Status            string
	Steps             []types.TaskStep
	Artifacts         []types.TaskArtifact
	LastError         string
	OtelStatusCode    string
	OtelStatusMessage string
}

type StubExecutor struct{}

func NewStubExecutor() *StubExecutor {
	return &StubExecutor{}
}

func (e *StubExecutor) Execute(_ context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}

	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "model",
		Title:    "Stub planning step",
		Status:   "completed",
		Phase:    "planning",
		Result:   telemetry.ResultSuccess,
		ToolName: "builtin.stub_planner",
		Input: map[string]any{
			"title":  spec.Task.Title,
			"prompt": spec.Task.Prompt,
		},
		OutputSummary: map[string]any{
			"summary":     "Stub orchestrator generated a first planning step.",
			"next_action": "review generated summary artifact",
		},
		StartedAt:  spec.StartedAt,
		FinishedAt: spec.StartedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}

	summary := fmt.Sprintf("Stub run %d for task %q created a first planning step and is ready for a real executor.", spec.Run.Number, spec.Task.Title)
	artifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "summary",
		Name:        "run-summary.txt",
		Description: "Stub run summary artifact",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: summary,
		SizeBytes:   int64(len(summary)),
		Status:      "ready",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}

	return &ExecutionResult{
		Status:         "completed",
		Steps:          []types.TaskStep{step},
		Artifacts:      []types.TaskArtifact{artifact},
		OtelStatusCode: "ok",
	}, nil
}

type ShellExecutor struct {
	sandbox sandbox.Executor
}

func NewShellExecutor(exec sandbox.Executor) *ShellExecutor {
	if exec == nil {
		exec = sandbox.NewLocalExecutor()
	}
	return &ShellExecutor{sandbox: exec}
}

func (e *ShellExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	command := spec.Task.ShellCommand
	if command == "" {
		return nil, fmt.Errorf("shell command is required")
	}

	timeout := spec.Task.TimeoutMS
	if timeout <= 0 {
		timeout = 5000
	}
	workingDirectory := spec.Task.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory = "."
	}
	resultData, err := e.sandbox.Run(ctx, sandbox.Command{
		Command:          command,
		WorkingDirectory: workingDirectory,
		Timeout:          time.Duration(timeout) * time.Millisecond,
	})

	status := "completed"
	result := telemetry.ResultSuccess
	lastError := ""
	otelStatusCode := "ok"
	otelStatusMessage := ""
	if err != nil {
		status = "failed"
		result = telemetry.ResultError
		lastError = err.Error()
		otelStatusCode = "error"
		otelStatusMessage = err.Error()
	}

	finishedAt := time.Now().UTC()
	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "shell",
		Title:    "Shell command",
		Status:   status,
		Phase:    "execution",
		Result:   result,
		ToolName: "shell",
		Input: map[string]any{
			"command":           command,
			"working_directory": workingDirectory,
			"timeout_ms":        timeout,
		},
		OutputSummary: map[string]any{
			"stdout_bytes": len(resultData.Stdout),
			"stderr_bytes": len(resultData.Stderr),
			"exit_code":    resultData.ExitCode,
		},
		ExitCode:   resultData.ExitCode,
		Error:      lastError,
		ErrorKind:  shellErrorKind(err),
		StartedAt:  spec.StartedAt,
		FinishedAt: finishedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}

	stdoutArtifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "stdout",
		Name:        "stdout.txt",
		Description: "Shell stdout capture",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: resultData.Stdout,
		SizeBytes:   int64(len(resultData.Stdout)),
		Status:      "ready",
		CreatedAt:   finishedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}

	stderrArtifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "stderr",
		Name:        "stderr.txt",
		Description: "Shell stderr capture",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: resultData.Stderr,
		SizeBytes:   int64(len(resultData.Stderr)),
		Status:      "ready",
		CreatedAt:   finishedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}

	return &ExecutionResult{
		Status:            status,
		Steps:             []types.TaskStep{step},
		Artifacts:         []types.TaskArtifact{stdoutArtifact, stderrArtifact},
		LastError:         lastError,
		OtelStatusCode:    otelStatusCode,
		OtelStatusMessage: otelStatusMessage,
	}, nil
}

func shellErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "shell_timeout"
	}
	return "shell_command_failed"
}
