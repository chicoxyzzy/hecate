package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor interface {
	Run(ctx context.Context, command Command) (Result, error)
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
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err == nil {
		return result, nil
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
