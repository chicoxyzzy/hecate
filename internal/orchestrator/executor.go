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
	return &ShellExecutor{sandbox: ensureSandboxExecutor(exec)}
}

func (e *ShellExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	command := spec.Task.ShellCommand
	if command == "" {
		return nil, fmt.Errorf("shell command is required")
	}
	return executeStreamingCommand(ctx, e.sandbox, spec, streamingCommandSpec{
		command:           command,
		kind:              "shell",
		title:             "Shell command",
		toolName:          "shell",
		stdoutName:        "stdout.txt",
		stdoutDescription: "Shell stdout capture",
		stderrName:        "stderr.txt",
		stderrDescription: "Shell stderr capture",
		timeoutErrorKind:  "shell_timeout",
		defaultErrorKind:  "shell_command_failed",
	})
}

func shellErrorKind(err error) string {
	return commandErrorKind(err, "shell_timeout", "shell_command_failed")
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
	return &GitExecutor{sandbox: ensureSandboxExecutor(exec)}
}

func (e *GitExecutor) Execute(ctx context.Context, spec ExecutionSpec) (*ExecutionResult, error) {
	if spec.NewID == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	command := spec.Task.GitCommand
	if command == "" {
		return nil, fmt.Errorf("git command is required")
	}
	return executeStreamingCommand(ctx, e.sandbox, spec, streamingCommandSpec{
		command:           "git " + command,
		displayCommand:    command,
		kind:              "git",
		title:             "Git command",
		toolName:          "git",
		stdoutName:        "git-stdout.txt",
		stdoutDescription: "Git stdout capture",
		stderrName:        "git-stderr.txt",
		stderrDescription: "Git stderr capture",
		timeoutErrorKind:  "git_timeout",
		defaultErrorKind:  "git_command_failed",
	})
}

func gitErrorKind(err error) string {
	return commandErrorKind(err, "git_timeout", "git_command_failed")
}

type streamingCommandSpec struct {
	command           string
	displayCommand    string
	kind              string
	title             string
	toolName          string
	stdoutName        string
	stdoutDescription string
	stderrName        string
	stderrDescription string
	timeoutErrorKind  string
	defaultErrorKind  string
}

func executeStreamingCommand(ctx context.Context, exec sandbox.Executor, spec ExecutionSpec, commandSpec streamingCommandSpec) (*ExecutionResult, error) {
	timeout := commandTimeout(spec.Task)
	workingDirectory := commandWorkingDirectory(spec.Task)
	displayCommand := commandSpec.displayCommand
	if displayCommand == "" {
		displayCommand = commandSpec.command
	}

	step := types.TaskStep{
		ID:       spec.NewID("step"),
		TaskID:   spec.Task.ID,
		RunID:    spec.Run.ID,
		Index:    1,
		Kind:     commandSpec.kind,
		Title:    commandSpec.title,
		Status:   "running",
		Phase:    "execution",
		Result:   telemetry.ResultSuccess,
		ToolName: commandSpec.toolName,
		Input: map[string]any{
			"command":           displayCommand,
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
	if err := upsertTaskStep(spec, step); err != nil {
		return nil, err
	}

	stdoutArtifact := newStreamingCommandArtifact(spec, step.ID, "stdout", commandSpec.stdoutName, commandSpec.stdoutDescription)
	if err := upsertTaskArtifact(spec, stdoutArtifact); err != nil {
		return nil, err
	}
	stderrArtifact := newStreamingCommandArtifact(spec, step.ID, "stderr", commandSpec.stderrName, commandSpec.stderrDescription)
	if err := upsertTaskArtifact(spec, stderrArtifact); err != nil {
		return nil, err
	}

	resultData, err := exec.RunStreaming(ctx, sandbox.Command{
		Command:          commandSpec.command,
		WorkingDirectory: workingDirectory,
		Timeout:          time.Duration(timeout) * time.Millisecond,
		Policy:           taskPolicy(spec.Task),
	}, func(chunk sandbox.OutputChunk) {
		switch chunk.Stream {
		case "stdout":
			stdoutArtifact.ContentText += chunk.Text
			stdoutArtifact.SizeBytes = int64(len(stdoutArtifact.ContentText))
			_ = upsertTaskArtifact(spec, stdoutArtifact)
		case "stderr":
			stderrArtifact.ContentText += chunk.Text
			stderrArtifact.SizeBytes = int64(len(stderrArtifact.ContentText))
			_ = upsertTaskArtifact(spec, stderrArtifact)
		}
	})

	status, result, lastError, otelStatusCode, otelStatusMessage := executionStatus(err)
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
	step.ErrorKind = commandErrorKind(err, commandSpec.timeoutErrorKind, commandSpec.defaultErrorKind)
	step.FinishedAt = finishedAt
	if err := upsertTaskStep(spec, step); err != nil {
		return nil, err
	}

	finalArtifactStatus := "ready"
	if status == "cancelled" {
		finalArtifactStatus = "cancelled"
	}
	stdoutArtifact.Status = finalArtifactStatus
	stderrArtifact.Status = finalArtifactStatus
	if err := upsertTaskArtifact(spec, stdoutArtifact); err != nil {
		return nil, err
	}
	if err := upsertTaskArtifact(spec, stderrArtifact); err != nil {
		return nil, err
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

func ensureSandboxExecutor(exec sandbox.Executor) sandbox.Executor {
	if exec == nil {
		return sandbox.NewLocalExecutor()
	}
	return exec
}

func commandTimeout(task types.Task) int {
	timeout := task.TimeoutMS
	if timeout <= 0 {
		return 5000
	}
	return timeout
}

func commandWorkingDirectory(task types.Task) string {
	if task.WorkingDirectory == "" {
		return "."
	}
	return task.WorkingDirectory
}

func executionStatus(err error) (status string, result string, lastError string, otelStatusCode string, otelStatusMessage string) {
	if err == nil {
		return "completed", telemetry.ResultSuccess, "", "ok", ""
	}

	status = "failed"
	result = telemetry.ResultError
	lastError = err.Error()
	otelStatusCode = "error"
	otelStatusMessage = err.Error()
	if errors.Is(err, context.Canceled) {
		status = "cancelled"
	}
	return status, result, lastError, otelStatusCode, otelStatusMessage
}

func commandErrorKind(err error, timeoutErrorKind, defaultErrorKind string) string {
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
		return timeoutErrorKind
	}
	return defaultErrorKind
}

func newStreamingCommandArtifact(spec ExecutionSpec, stepID, kind, name, description string) types.TaskArtifact {
	return types.TaskArtifact{
		ID:          spec.NewID("artifact"),
		TaskID:      spec.Task.ID,
		RunID:       spec.Run.ID,
		StepID:      stepID,
		Kind:        kind,
		Name:        name,
		Description: description,
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: "",
		SizeBytes:   0,
		Status:      "streaming",
		CreatedAt:   spec.StartedAt,
		RequestID:   spec.RequestID,
		TraceID:     spec.TraceID,
	}
}

func upsertTaskStep(spec ExecutionSpec, step types.TaskStep) error {
	if spec.UpsertStep == nil {
		return nil
	}
	return spec.UpsertStep(step)
}

func upsertTaskArtifact(spec ExecutionSpec, artifact types.TaskArtifact) error {
	if spec.UpsertArtifact == nil {
		return nil
	}
	return spec.UpsertArtifact(artifact)
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
