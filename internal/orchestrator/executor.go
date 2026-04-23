package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/hecate/agent-runtime/internal/sandbox"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Executor interface {
	Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error)
}

type ExecutionSpec struct {
	Task           types.Task
	Run            types.TaskRun
	RequestID      string
	TraceID        string
	RootSpanID     string
	StartedAt      time.Time
	NewID          func(prefix string) string
	UpsertStep     func(step types.TaskStep) error
	UpsertArtifact func(artifact types.TaskArtifact) error
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
	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "shell",
		Title:    "Shell command",
		Status:   "running",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: "shell",
		Input: map[string]any{
			"command":           command,
			"working_directory": workingDirectory,
			"timeout_ms":        timeout,
		},
		OutputSummary: map[string]any{
			"stdout_bytes": 0,
			"stderr_bytes": 0,
			"exit_code":    0,
		},
		StartedAt: spec.StartedAt,
		RequestID: spec.RequestID,
		TraceID:   spec.TraceID,
	}
	if spec.UpsertStep != nil {
		if err := spec.UpsertStep(step); err != nil {
			return nil, err
		}
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
		ContentText: "",
		SizeBytes:   0,
		Status:      "streaming",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stdoutArtifact); err != nil {
			return nil, err
		}
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
		ContentText: "",
		SizeBytes:   0,
		Status:      "streaming",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stderrArtifact); err != nil {
			return nil, err
		}
	}

	resultData, err := e.sandbox.RunStreaming(ctx, sandbox.Command{
		Command:          command,
		WorkingDirectory: workingDirectory,
		Timeout:          time.Duration(timeout) * time.Millisecond,
		Policy:           taskPolicy(spec.Task),
	}, func(chunk sandbox.OutputChunk) {
		switch chunk.Stream {
		case "stdout":
			stdoutArtifact.ContentText += chunk.Text
			stdoutArtifact.SizeBytes = int64(len(stdoutArtifact.ContentText))
			if spec.UpsertArtifact != nil {
				_ = spec.UpsertArtifact(stdoutArtifact)
			}
		case "stderr":
			stderrArtifact.ContentText += chunk.Text
			stderrArtifact.SizeBytes = int64(len(stderrArtifact.ContentText))
			if spec.UpsertArtifact != nil {
				_ = spec.UpsertArtifact(stderrArtifact)
			}
		}
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
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
	}

	finishedAt := time.Now().UTC()
	step.Status = status
	step.Result = result
	step.OutputSummary = map[string]any{
		"stdout_bytes": len(resultData.Stdout),
		"stderr_bytes": len(resultData.Stderr),
		"exit_code":    resultData.ExitCode,
	}
	step.ExitCode = resultData.ExitCode
	step.Error = lastError
	step.ErrorKind = shellErrorKind(err)
	step.FinishedAt = finishedAt
	if spec.UpsertStep != nil {
		if err := spec.UpsertStep(step); err != nil {
			return nil, err
		}
	}
	stdoutArtifact.Status = "ready"
	stderrArtifact.Status = "ready"
	if status == "cancelled" {
		stdoutArtifact.Status = "cancelled"
		stderrArtifact.Status = "cancelled"
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stdoutArtifact); err != nil {
			return nil, err
		}
		if err := spec.UpsertArtifact(stderrArtifact); err != nil {
			return nil, err
		}
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
	if errors.Is(err, context.Canceled) {
		return "run_cancelled"
	}
	if sandbox.IsPolicyDenied(err) {
		return "sandbox_policy_denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "shell_timeout"
	}
	return "shell_command_failed"
}

type FileExecutor struct {
	sandbox sandbox.Executor
}

func NewFileExecutor(exec sandbox.Executor) *FileExecutor {
	if exec == nil {
		exec = sandbox.NewLocalExecutor()
	}
	return &FileExecutor{sandbox: exec}
}

func (e *FileExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	if spec.Task.FilePath == "" {
		return nil, fmt.Errorf("file path is required")
	}

	operation := spec.Task.FileOperation
	if operation == "" {
		operation = "write"
	}
	if spec.Task.SandboxReadOnly {
		return fileFailure(spec, operation, spec.Task.FilePath, "sandbox policy denied: write access is disabled", "sandbox_policy_denied"), nil
	}
	request := sandbox.FileRequest{
		Path:             spec.Task.FilePath,
		Content:          spec.Task.FileContent,
		WorkingDirectory: spec.Task.WorkingDirectory,
		Policy:           taskPolicy(spec.Task),
	}
	var (
		fileResult sandbox.FileResult
		err        error
	)
	switch operation {
	case "write":
		fileResult, err = e.sandbox.WriteFile(ctx, request)
	case "append":
		fileResult, err = e.sandbox.AppendFile(ctx, request)
	default:
		return fileFailure(spec, operation, spec.Task.FilePath, fmt.Sprintf("unsupported file operation %q", operation), "file_operation_unsupported"), nil
	}
	if err != nil {
		return fileFailure(spec, operation, spec.Task.FilePath, err.Error(), fileErrorKind(err)), nil
	}

	finishedAt := time.Now().UTC()
	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "file",
		Title:    "File operation",
		Status:   "completed",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: "file",
		Input: map[string]any{
			"operation":         operation,
			"path":              spec.Task.FilePath,
			"working_directory": spec.Task.WorkingDirectory,
		},
		OutputSummary: map[string]any{
			"path":  fileResult.Path,
			"bytes": fileResult.BytesWritten,
		},
		StartedAt:  spec.StartedAt,
		FinishedAt: finishedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	artifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "file",
		Name:        filepath.Base(fileResult.Path),
		Description: "File executor output",
		MimeType:    "text/plain",
		StorageKind: "inline",
		Path:        fileResult.Path,
		ContentText: spec.Task.FileContent,
		SizeBytes:   int64(len(spec.Task.FileContent)),
		Status:      "ready",
		CreatedAt:   finishedAt,
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

type GitExecutor struct {
	sandbox sandbox.Executor
}

func NewGitExecutor(exec sandbox.Executor) *GitExecutor {
	if exec == nil {
		exec = sandbox.NewLocalExecutor()
	}
	return &GitExecutor{sandbox: exec}
}

func (e *GitExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	command := spec.Task.GitCommand
	if command == "" {
		return nil, fmt.Errorf("git command is required")
	}

	timeout := spec.Task.TimeoutMS
	if timeout <= 0 {
		timeout = 5000
	}
	workingDirectory := spec.Task.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory = "."
	}
	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "git",
		Title:    "Git command",
		Status:   "running",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: "git",
		Input: map[string]any{
			"command":           command,
			"working_directory": workingDirectory,
			"timeout_ms":        timeout,
		},
		OutputSummary: map[string]any{
			"stdout_bytes": 0,
			"stderr_bytes": 0,
			"exit_code":    0,
		},
		StartedAt: spec.StartedAt,
		RequestID: spec.RequestID,
		TraceID:   spec.TraceID,
	}
	if spec.UpsertStep != nil {
		if err := spec.UpsertStep(step); err != nil {
			return nil, err
		}
	}

	stdoutArtifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "stdout",
		Name:        "git-stdout.txt",
		Description: "Git stdout capture",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: "",
		SizeBytes:   0,
		Status:      "streaming",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stdoutArtifact); err != nil {
			return nil, err
		}
	}
	stderrArtifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "stderr",
		Name:        "git-stderr.txt",
		Description: "Git stderr capture",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: "",
		SizeBytes:   0,
		Status:      "streaming",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stderrArtifact); err != nil {
			return nil, err
		}
	}

	resultData, err := e.sandbox.RunStreaming(ctx, sandbox.Command{
		Command:          "git " + command,
		WorkingDirectory: workingDirectory,
		Timeout:          time.Duration(timeout) * time.Millisecond,
		Policy:           taskPolicy(spec.Task),
	}, func(chunk sandbox.OutputChunk) {
		switch chunk.Stream {
		case "stdout":
			stdoutArtifact.ContentText += chunk.Text
			stdoutArtifact.SizeBytes = int64(len(stdoutArtifact.ContentText))
			if spec.UpsertArtifact != nil {
				_ = spec.UpsertArtifact(stdoutArtifact)
			}
		case "stderr":
			stderrArtifact.ContentText += chunk.Text
			stderrArtifact.SizeBytes = int64(len(stderrArtifact.ContentText))
			if spec.UpsertArtifact != nil {
				_ = spec.UpsertArtifact(stderrArtifact)
			}
		}
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
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
	}

	finishedAt := time.Now().UTC()
	step.Status = status
	step.Result = result
	step.OutputSummary = map[string]any{
		"stdout_bytes": len(resultData.Stdout),
		"stderr_bytes": len(resultData.Stderr),
		"exit_code":    resultData.ExitCode,
	}
	step.ExitCode = resultData.ExitCode
	step.Error = lastError
	step.ErrorKind = gitErrorKind(err)
	step.FinishedAt = finishedAt
	if spec.UpsertStep != nil {
		if err := spec.UpsertStep(step); err != nil {
			return nil, err
		}
	}
	stdoutArtifact.Status = "ready"
	stderrArtifact.Status = "ready"
	if status == "cancelled" {
		stdoutArtifact.Status = "cancelled"
		stderrArtifact.Status = "cancelled"
	}
	if spec.UpsertArtifact != nil {
		if err := spec.UpsertArtifact(stdoutArtifact); err != nil {
			return nil, err
		}
		if err := spec.UpsertArtifact(stderrArtifact); err != nil {
			return nil, err
		}
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

func gitErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "run_cancelled"
	}
	if sandbox.IsPolicyDenied(err) {
		return "sandbox_policy_denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "git_timeout"
	}
	return "git_command_failed"
}

func fileErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if sandbox.IsPolicyDenied(err) {
		return "sandbox_policy_denied"
	}
	return "file_operation_failed"
}

func taskPolicy(task types.Task) sandbox.Policy {
	return sandbox.Policy{
		AllowedRoot: task.SandboxAllowedRoot,
		ReadOnly:    task.SandboxReadOnly,
		Network:     task.SandboxNetwork,
	}
}

func fileFailure(spec ExecutionSpec, operation, path, message, errorKind string) *ExecutionResult {
	finishedAt := time.Now().UTC()
	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     "file",
		Title:    "File operation",
		Status:   "failed",
		Phase:    "execution",
		Result:   telemetry.ResultError,
		ToolName: "file",
		Input: map[string]any{
			"operation":         operation,
			"path":              path,
			"working_directory": spec.Task.WorkingDirectory,
		},
		Error:      message,
		ErrorKind:  errorKind,
		StartedAt:  spec.StartedAt,
		FinishedAt: finishedAt,
		RequestID:  spec.RequestID,
		TraceID:    spec.TraceID,
	}
	artifact := types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      step.ID,
		Kind:        "stderr",
		Name:        "file-error.txt",
		Description: "File executor error output",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: message,
		SizeBytes:   int64(len(message)),
		Status:      "ready",
		CreatedAt:   finishedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
	return &ExecutionResult{
		Status:            "failed",
		Steps:             []types.TaskStep{step},
		Artifacts:         []types.TaskArtifact{artifact},
		LastError:         message,
		OtelStatusCode:    "error",
		OtelStatusMessage: message,
	}
}
