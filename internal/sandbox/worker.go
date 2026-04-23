package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	workerOperationRun        = "run"
	workerOperationWriteFile  = "write_file"
	workerOperationAppendFile = "append_file"
)

type WorkerExecutor struct{}

type workerRequest struct {
	Operation string       `json:"operation"`
	Command   *Command     `json:"command,omitempty"`
	File      *FileRequest `json:"file,omitempty"`
}

type workerResponse struct {
	Result     Result     `json:"result"`
	FileResult FileResult `json:"file_result"`
	Error      string     `json:"error,omitempty"`
	ErrorKind  string     `json:"error_kind,omitempty"`
}

type workerEvent struct {
	Type      string          `json:"type"`
	Stream    string          `json:"stream,omitempty"`
	Text      string          `json:"text,omitempty"`
	Result    Result          `json:"result"`
	Error     string          `json:"error,omitempty"`
	ErrorKind string          `json:"error_kind,omitempty"`
	Response  *workerResponse `json:"response,omitempty"`
}

var sandboxdBinary struct {
	once sync.Once
	path string
	err  error
}

func NewWorkerExecutor() *WorkerExecutor {
	return &WorkerExecutor{}
}

func (e *WorkerExecutor) Run(ctx context.Context, command Command) (Result, error) {
	return e.RunStreaming(ctx, command, nil)
}

func (e *WorkerExecutor) RunStreaming(ctx context.Context, command Command, onChunk func(OutputChunk)) (Result, error) {
	result, err := invokeStreamingWorker(ctx, workerRequest{
		Operation: workerOperationRun,
		Command:   &command,
	}, command.Timeout+5*time.Second, onChunk)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	return result, nil
}

func (e *WorkerExecutor) WriteFile(ctx context.Context, request FileRequest) (FileResult, error) {
	response, err := invokeWorker(ctx, workerRequest{
		Operation: workerOperationWriteFile,
		File:      &request,
	}, 5*time.Second)
	if err != nil {
		return FileResult{}, err
	}
	if response.Error != "" {
		return response.FileResult, decodeWorkerError(response.Error, response.ErrorKind)
	}
	return response.FileResult, nil
}

func (e *WorkerExecutor) AppendFile(ctx context.Context, request FileRequest) (FileResult, error) {
	response, err := invokeWorker(ctx, workerRequest{
		Operation: workerOperationAppendFile,
		File:      &request,
	}, 5*time.Second)
	if err != nil {
		return FileResult{}, err
	}
	if response.Error != "" {
		return response.FileResult, decodeWorkerError(response.Error, response.ErrorKind)
	}
	return response.FileResult, nil
}

func ServeWorker(ctx context.Context, input io.Reader, output io.Writer) error {
	var request workerRequest
	if err := json.NewDecoder(input).Decode(&request); err != nil {
		return err
	}

	executor := NewLocalExecutor()
	response := workerResponse{}

	switch request.Operation {
	case workerOperationRun:
		if request.Command == nil {
			return fmt.Errorf("command request is required")
		}
		encoder := json.NewEncoder(output)
		result, err := executor.RunStreaming(ctx, *request.Command, func(chunk OutputChunk) {
			_ = encoder.Encode(workerEvent{
				Type:   "chunk",
				Stream: chunk.Stream,
				Text:   chunk.Text,
			})
		})
		finalEvent := workerEvent{
			Type:   "result",
			Result: result,
		}
		if err != nil {
			finalEvent.Error = err.Error()
			finalEvent.ErrorKind = classifyWorkerError(err)
		}
		return encoder.Encode(finalEvent)
	case workerOperationWriteFile:
		if request.File == nil {
			return fmt.Errorf("file request is required")
		}
		result, err := executor.WriteFile(ctx, *request.File)
		response.FileResult = result
		if err != nil {
			response.Error = err.Error()
			response.ErrorKind = classifyWorkerError(err)
		}
	case workerOperationAppendFile:
		if request.File == nil {
			return fmt.Errorf("file request is required")
		}
		result, err := executor.AppendFile(ctx, *request.File)
		response.FileResult = result
		if err != nil {
			response.Error = err.Error()
			response.ErrorKind = classifyWorkerError(err)
		}
	default:
		return fmt.Errorf("unsupported worker operation %q", request.Operation)
	}

	return json.NewEncoder(output).Encode(workerEvent{
		Type:     "response",
		Response: &response,
	})
}

func invokeWorker(ctx context.Context, request workerRequest, defaultTimeout time.Duration) (workerResponse, error) {
	binaryPath, err := sandboxdBinaryPath()
	if err != nil {
		return workerResponse{}, err
	}

	execCtx := ctx
	cancel := func() {}
	if defaultTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, defaultTimeout)
	}
	defer cancel()

	payload, err := json.Marshal(request)
	if err != nil {
		return workerResponse{}, err
	}

	cmd := exec.CommandContext(execCtx, binaryPath, "worker")
	cmd.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return workerResponse{}, context.DeadlineExceeded
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return workerResponse{}, fmt.Errorf("sandbox worker failed: %s", message)
	}

	var event workerEvent
	if err := json.NewDecoder(&stdout).Decode(&event); err != nil {
		return workerResponse{}, err
	}
	if event.Response == nil {
		return workerResponse{}, fmt.Errorf("sandbox worker response missing")
	}
	return *event.Response, nil
}

func invokeStreamingWorker(ctx context.Context, request workerRequest, defaultTimeout time.Duration, onChunk func(OutputChunk)) (Result, error) {
	binaryPath, err := sandboxdBinaryPath()
	if err != nil {
		return Result{ExitCode: -1}, err
	}

	execCtx := ctx
	cancel := func() {}
	if defaultTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, defaultTimeout)
	}
	defer cancel()

	payload, err := json.Marshal(request)
	if err != nil {
		return Result{ExitCode: -1}, err
	}

	cmd := exec.CommandContext(execCtx, binaryPath, "worker")
	cmd.Stdin = bytes.NewReader(payload)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1}, err
	}

	decoder := json.NewDecoder(stdoutPipe)
	var finalResult Result
	var finalErr error
	for {
		var event workerEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			finalErr = err
			break
		}
		switch event.Type {
		case "chunk":
			if onChunk != nil {
				onChunk(OutputChunk{Stream: event.Stream, Text: event.Text})
			}
		case "result":
			finalResult = event.Result
			if event.Error != "" {
				finalErr = decodeWorkerError(event.Error, event.ErrorKind)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if errors.Is(execCtx.Err(), context.Canceled) {
			return finalResult, context.Canceled
		}
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return finalResult, context.DeadlineExceeded
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return finalResult, fmt.Errorf("sandbox worker failed: %s", message)
	}
	return finalResult, finalErr
}

func classifyWorkerError(err error) string {
	switch {
	case IsPolicyDenied(err):
		return "policy"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "generic"
	}
}

func decodeWorkerError(message, kind string) error {
	switch kind {
	case "policy":
		reason := strings.TrimSpace(message)
		reason = strings.TrimPrefix(reason, "sandbox policy denied:")
		reason = strings.TrimSpace(reason)
		return &PolicyError{Reason: reason}
	case "timeout":
		return context.DeadlineExceeded
	default:
		return fmt.Errorf("%s", message)
	}
}

func sandboxdBinaryPath() (string, error) {
	sandboxdBinary.once.Do(func() {
		cacheDir := filepath.Join(os.TempDir(), "hecate-sandboxd-cache")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			sandboxdBinary.err = err
			return
		}
		sandboxdBinary.path = filepath.Join(cacheDir, "sandboxd")
		cmd := exec.Command("go", "build", "-o", sandboxdBinary.path, filepath.Join(repoRoot(), "cmd", "sandboxd"))
		if output, err := cmd.CombinedOutput(); err != nil {
			sandboxdBinary.err = fmt.Errorf("build sandboxd: %w: %s", err, string(output))
			return
		}
	})
	return sandboxdBinary.path, sandboxdBinary.err
}

func repoRoot() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
