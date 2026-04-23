package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Policy struct {
	AllowedRoot string
	ReadOnly    bool
	Network     bool
}

type Command struct {
	Command          string
	WorkingDirectory string
	Timeout          time.Duration
	Policy           Policy
}

type FileRequest struct {
	Path             string
	Content          string
	WorkingDirectory string
	Policy           Policy
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type FileResult struct {
	Path         string
	BytesWritten int
}

type OutputChunk struct {
	Stream string
	Text   string
}

type Executor interface {
	Run(ctx context.Context, command Command) (Result, error)
	RunStreaming(ctx context.Context, command Command, onChunk func(OutputChunk)) (Result, error)
	WriteFile(ctx context.Context, request FileRequest) (FileResult, error)
	AppendFile(ctx context.Context, request FileRequest) (FileResult, error)
}

type PolicyError struct {
	Reason string
}

func (e *PolicyError) Error() string {
	if e == nil || strings.TrimSpace(e.Reason) == "" {
		return "sandbox policy denied"
	}
	return "sandbox policy denied: " + e.Reason
}

func IsPolicyDenied(err error) bool {
	var policyErr *PolicyError
	return errors.As(err, &policyErr)
}

type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Run(ctx context.Context, command Command) (Result, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	return e.RunStreaming(ctx, command, func(chunk OutputChunk) {
		switch chunk.Stream {
		case "stdout":
			stdout.WriteString(chunk.Text)
		case "stderr":
			stderr.WriteString(chunk.Text)
		}
	})
}

func (e *LocalExecutor) RunStreaming(ctx context.Context, command Command, onChunk func(OutputChunk)) (Result, error) {
	workingDirectory, err := resolveWorkingDirectory(command.WorkingDirectory, command.Policy)
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	if err := validateCommand(command.Command, command.Policy); err != nil {
		return Result{ExitCode: -1}, err
	}

	runCtx := ctx
	cancel := func() {}
	if command.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, command.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-lc", command.Command)
	if workingDirectory != "" {
		cmd.Dir = workingDirectory
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{ExitCode: -1}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: -1}, err
	}

	readDone := make(chan error, 2)
	go streamPipe(stdoutPipe, "stdout", &stdout, onChunk, readDone)
	go streamPipe(stderrPipe, "stderr", &stderr, onChunk, readDone)

	err = cmd.Wait()
	firstReadErr := <-readDone
	secondReadErr := <-readDone
	if firstReadErr != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, firstReadErr
	}
	if secondReadErr != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1}, secondReadErr
	}
	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err == nil {
		return result, nil
	}

	if errors.Is(runCtx.Err(), context.Canceled) {
		result.ExitCode = -1
		return result, context.Canceled
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		return result, runCtx.Err()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	result.ExitCode = -1
	return result, err
}

func (e *LocalExecutor) WriteFile(_ context.Context, request FileRequest) (FileResult, error) {
	return writeFile(request, false)
}

func (e *LocalExecutor) AppendFile(_ context.Context, request FileRequest) (FileResult, error) {
	return writeFile(request, true)
}

func writeFile(request FileRequest, appendMode bool) (FileResult, error) {
	if request.Policy.ReadOnly {
		return FileResult{}, &PolicyError{Reason: "write access is disabled"}
	}
	targetPath, err := ResolvePath(request.WorkingDirectory, request.Path, request.Policy)
	if err != nil {
		return FileResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return FileResult{}, err
	}
	if !appendMode {
		if err := os.WriteFile(targetPath, []byte(request.Content), 0o644); err != nil {
			return FileResult{}, err
		}
		return FileResult{Path: targetPath, BytesWritten: len(request.Content)}, nil
	}
	handle, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return FileResult{}, err
	}
	defer handle.Close()
	if _, err := io.WriteString(handle, request.Content); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: targetPath, BytesWritten: len(request.Content)}, nil
}

func streamPipe(pipe io.ReadCloser, streamName string, sink *bytes.Buffer, onChunk func(OutputChunk), done chan<- error) {
	reader := bufio.NewReader(pipe)
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			text := string(buffer[:n])
			sink.WriteString(text)
			if onChunk != nil {
				onChunk(OutputChunk{Stream: streamName, Text: text})
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			done <- nil
			return
		}
		if errors.Is(err, os.ErrClosed) {
			done <- nil
			return
		}
		done <- err
		return
	}
}

func ResolvePath(workingDirectory, targetPath string, policy Policy) (string, error) {
	if strings.TrimSpace(targetPath) == "" {
		return "", fmt.Errorf("target path is required")
	}

	baseDirectory := strings.TrimSpace(workingDirectory)
	if baseDirectory == "" {
		baseDirectory = "."
	}
	var err error
	baseDirectory, err = filepath.Abs(baseDirectory)
	if err != nil {
		return "", err
	}

	resolvedPath := targetPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(baseDirectory, resolvedPath)
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", err
	}

	if err := ensureWithinAllowedRoot(resolvedPath, policy); err != nil {
		return "", err
	}
	return resolvedPath, nil
}

func resolveWorkingDirectory(workingDirectory string, policy Policy) (string, error) {
	if strings.TrimSpace(workingDirectory) == "" {
		if strings.TrimSpace(policy.AllowedRoot) == "" {
			return "", nil
		}
		workingDirectory = policy.AllowedRoot
	}
	resolvedDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return "", err
	}
	if err := ensureWithinAllowedRoot(resolvedDirectory, policy); err != nil {
		return "", err
	}
	return resolvedDirectory, nil
}

func ensureWithinAllowedRoot(path string, policy Policy) error {
	allowedRoot := strings.TrimSpace(policy.AllowedRoot)
	if allowedRoot == "" {
		return nil
	}
	resolvedRoot, err := filepath.Abs(allowedRoot)
	if err != nil {
		return err
	}
	relativePath, err := filepath.Rel(resolvedRoot, path)
	if err != nil {
		return err
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return &PolicyError{Reason: fmt.Sprintf("path %q escapes allowed root %q", path, resolvedRoot)}
	}
	return nil
}

func validateCommand(command string, policy Policy) error {
	if !policy.Network && commandRequestsNetwork(command) {
		return &PolicyError{Reason: "network access is disabled"}
	}
	if policy.ReadOnly && commandMutatesState(command) {
		return &PolicyError{Reason: "write access is disabled"}
	}
	return nil
}

func commandRequestsNetwork(command string) bool {
	lowerCommand := strings.ToLower(command)
	patterns := []string{
		"curl ",
		"wget ",
		"ping ",
		"ssh ",
		"scp ",
		"nc ",
		"netcat ",
		"telnet ",
		"ftp ",
		"http://",
		"https://",
		"git clone ",
		"git fetch ",
		"git pull ",
		"git push ",
		"git ls-remote ",
	}
	return containsAnyPattern(lowerCommand, patterns)
}

func commandMutatesState(command string) bool {
	lowerCommand := strings.ToLower(command)
	patterns := []string{
		"rm ",
		"mv ",
		"cp ",
		"mkdir ",
		"touch ",
		"tee ",
		"sed -i",
		"git add ",
		"git apply ",
		"git cherry-pick ",
		"git clean ",
		"git commit ",
		"git merge ",
		"git push ",
		"git rebase ",
		"git restore ",
		"git revert ",
		"git switch ",
		"git checkout ",
	}
	if containsAnyPattern(lowerCommand, patterns) {
		return true
	}
	return strings.Contains(lowerCommand, " >") || strings.Contains(lowerCommand, ">>")
}

func containsAnyPattern(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(value, pattern) {
			return true
		}
	}
	return false
}
