package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type Command struct {
	Command          string
	WorkingDirectory string
	Timeout          time.Duration
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor interface {
	Run(ctx context.Context, command Command) (Result, error)
}

type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) Run(ctx context.Context, command Command) (Result, error) {
	runCtx := ctx
	cancel := func() {}
	if command.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, command.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-lc", command.Command)
	if command.WorkingDirectory != "" {
		cmd.Dir = command.WorkingDirectory
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
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
