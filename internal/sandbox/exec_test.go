package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalExecutorSeparatesStdoutAndStderr(t *testing.T) {
	t.Parallel()

	exec := NewLocalExecutor()
	result, err := exec.Run(context.Background(), Command{
		Command: `printf 'hello stdout'; printf 'hello stderr' >&2`,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != "hello stdout" {
		t.Fatalf("stdout = %q, want hello stdout", result.Stdout)
	}
	if result.Stderr != "hello stderr" {
		t.Fatalf("stderr = %q, want hello stderr", result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", result.ExitCode)
	}
}

func TestLocalExecutorTimeout(t *testing.T) {
	t.Parallel()

	exec := NewLocalExecutor()
	result, err := exec.Run(context.Background(), Command{
		Command: `sleep 1`,
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("exit_code = %d, want -1", result.ExitCode)
	}
}

func TestLocalExecutorDeniedByNetworkPolicy(t *testing.T) {
	t.Parallel()

	exec := NewLocalExecutor()
	result, err := exec.Run(context.Background(), Command{
		Command: `curl https://example.com`,
		Policy:  Policy{Network: false},
		Timeout: time.Second,
	})
	if !IsPolicyDenied(err) {
		t.Fatalf("Run() error = %v, want policy denial", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("exit_code = %d, want -1", result.ExitCode)
	}
}

func TestLocalExecutorDeniedByReadOnlyPolicy(t *testing.T) {
	t.Parallel()

	exec := NewLocalExecutor()
	result, err := exec.Run(context.Background(), Command{
		Command: `touch denied.txt`,
		Policy:  Policy{ReadOnly: true},
		Timeout: time.Second,
	})
	if !IsPolicyDenied(err) {
		t.Fatalf("Run() error = %v, want policy denial", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("exit_code = %d, want -1", result.ExitCode)
	}
}

func TestResolvePathRejectsEscapeFromAllowedRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workingDirectory := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workingDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	_, err := ResolvePath(workingDirectory, "../outside.txt", Policy{AllowedRoot: workingDirectory})
	if !IsPolicyDenied(err) {
		t.Fatalf("ResolvePath() error = %v, want policy denial", err)
	}
}
