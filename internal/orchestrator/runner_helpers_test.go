package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestFirstNonEmptyTrimsAndPicksFirst(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"all empty", []string{"", "  ", "\t"}, ""},
		{"first wins", []string{"first", "second"}, "first"},
		{"trims whitespace", []string{"  trimmed  "}, "trimmed"},
		{"skips empty until first non-empty", []string{"", "  ", "third"}, "third"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstNonEmpty(tc.in...); got != tc.want {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaxIntFallsBackOnNonPositive(t *testing.T) {
	cases := []struct {
		value, fallback, want int
	}{
		{0, 7, 7},
		{-1, 7, 7},
		{12, 7, 12},
	}
	for _, tc := range cases {
		if got := maxInt(tc.value, tc.fallback); got != tc.want {
			t.Errorf("maxInt(%d, %d) = %d, want %d", tc.value, tc.fallback, got, tc.want)
		}
	}
}

func TestFindOldestRunStartIgnoresZeroTimes(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		runs []types.TaskRun
		want time.Time
	}{
		{"empty returns zero", nil, time.Time{}},
		{"all zero returns zero", []types.TaskRun{{}, {}}, time.Time{}},
		{"picks earliest non-zero", []types.TaskRun{{StartedAt: t2}, {StartedAt: t1}, {StartedAt: t3}}, t1},
		{"skips zero entries", []types.TaskRun{{}, {StartedAt: t2}, {}}, t2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findOldestRunStart(tc.runs)
			if !got.Equal(tc.want) {
				t.Errorf("findOldestRunStart = %v, want %v", got, tc.want)
			}
		})
	}
}

// runnerWithPolicies builds a minimally-initialized Runner that bypasses
// NewRunner — we only need the policies map and executor pointers for the
// helpers under test, not the goroutines and queue NewRunner spins up.
func runnerWithPolicies(policies ...string) *Runner {
	r := &Runner{policies: make(map[string]struct{})}
	for _, p := range policies {
		r.policies[p] = struct{}{}
	}
	return r
}

func TestApprovalSpecForTask(t *testing.T) {
	cases := []struct {
		name       string
		policies   []string
		task       types.Task
		wantKind   string
		wantReason bool
	}{
		{
			name:       "shell policy + matching task → shell_command",
			policies:   []string{"shell_exec"},
			task:       types.Task{ExecutionKind: "shell", ShellCommand: "ls"},
			wantKind:   "shell_command",
			wantReason: true,
		},
		{
			name:     "shell task without policy → no approval",
			policies: nil,
			task:     types.Task{ExecutionKind: "shell", ShellCommand: "ls"},
			wantKind: "",
		},
		{
			name:       "git policy + matching task → git_exec",
			policies:   []string{"git_exec"},
			task:       types.Task{ExecutionKind: "git", GitCommand: "status"},
			wantKind:   "git_exec",
			wantReason: true,
		},
		{
			name:       "file policy + matching task → file_write",
			policies:   []string{"file_write"},
			task:       types.Task{ExecutionKind: "file", FilePath: "/tmp/x"},
			wantKind:   "file_write",
			wantReason: true,
		},
		{
			name:       "network egress policy fires regardless of execution kind",
			policies:   []string{"network_egress"},
			task:       types.Task{ExecutionKind: "shell", ShellCommand: "ls", SandboxNetwork: true},
			wantKind:   "network_egress",
			wantReason: true,
		},
		{
			name:     "shell task with empty command does not require approval",
			policies: []string{"shell_exec"},
			task:     types.Task{ExecutionKind: "shell", ShellCommand: "  "},
			wantKind: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runnerWithPolicies(tc.policies...)
			kind, reason := r.approvalSpecForTask(tc.task)
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
			if (reason != "") != tc.wantReason {
				t.Errorf("reason presence = %v, want %v (got %q)", reason != "", tc.wantReason, reason)
			}
			if got := r.approvalRequiredForTask(tc.task); got != tc.wantReason {
				t.Errorf("approvalRequiredForTask = %v, want %v", got, tc.wantReason)
			}
		})
	}
}

// stubExec is a minimal Executor implementation used as a sentinel so
// executorForTask's branch selection can be observed without invoking real
// sandbox code.
type stubExec struct{ name string }

func (s *stubExec) Execute(context.Context, ExecutionSpec) (*ExecutionResult, error) {
	return nil, nil
}

func TestExecutorForTaskRouting(t *testing.T) {
	exec := &stubExec{name: "exec"}
	shell := &stubExec{name: "shell"}
	file := &stubExec{name: "file"}
	git := &stubExec{name: "git"}
	agent := &stubExec{name: "agent"}

	r := &Runner{exec: exec, shell: shell, file: file, git: git, agent: agent}

	cases := []struct {
		name string
		task types.Task
		want Executor
	}{
		{"agent_loop routes to agent", types.Task{ExecutionKind: "agent_loop"}, agent},
		{"shell with command routes to shell", types.Task{ExecutionKind: "shell", ShellCommand: "ls"}, shell},
		{"shell with empty command falls through to default exec", types.Task{ExecutionKind: "shell"}, exec},
		{"file with path routes to file", types.Task{ExecutionKind: "file", FilePath: "/tmp/x"}, file},
		{"git with command routes to git", types.Task{ExecutionKind: "git", GitCommand: "status"}, git},
		{"unknown kind falls through to default exec", types.Task{ExecutionKind: "weird"}, exec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.executorForTask(tc.task)
			if got != tc.want {
				t.Errorf("executorForTask kind=%q got=%v, want %v", tc.task.ExecutionKind, got, tc.want)
			}
		})
	}
}
