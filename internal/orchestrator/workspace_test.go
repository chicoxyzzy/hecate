package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestIsGitRepositoryDetectsDotGitDir(t *testing.T) {
	dir := t.TempDir()

	if isGitRepository(dir) {
		t.Fatal("plain temp dir reported as git repo")
	}

	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if !isGitRepository(dir) {
		t.Error("temp dir with .git/ not detected as git repo")
	}

	// .git as a file (e.g. submodule pointer) is intentionally not treated as
	// a repo by isGitRepository — it requires .git to be a directory so the
	// orchestrator can `git clone --no-hardlinks` from it.
	plainDir := t.TempDir()
	gitFile := filepath.Join(plainDir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: ../foo"), 0o644); err != nil {
		t.Fatalf("write .git file: %v", err)
	}
	if isGitRepository(plainDir) {
		t.Error(".git regular file should not be treated as a repository")
	}
}

func TestWorkspaceSourcePrefersWorkingDirectoryOverRepo(t *testing.T) {
	wd := t.TempDir()
	repo := t.TempDir()
	// Mark the repo as a git directory so it would otherwise be picked.
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo/.git: %v", err)
	}

	got := workspaceSource(types.Task{WorkingDirectory: wd, Repo: repo})
	if got.path != wd {
		t.Errorf("path = %q, want %q (working directory should win)", got.path, wd)
	}
	if got.kind != "directory" {
		t.Errorf("kind = %q, want %q", got.kind, "directory")
	}
}

func TestWorkspaceSourceClassifiesGitRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	got := workspaceSource(types.Task{Repo: repo})
	if got.kind != "git" {
		t.Errorf("kind = %q, want %q", got.kind, "git")
	}
	if got.path != repo {
		t.Errorf("path = %q, want %q", got.path, repo)
	}
}

func TestWorkspaceSourceRejectsRelativeAndMissingPaths(t *testing.T) {
	cases := []struct {
		name string
		task types.Task
	}{
		{"empty task", types.Task{}},
		{"relative working directory", types.Task{WorkingDirectory: "./relative"}},
		{"non-existent absolute path", types.Task{WorkingDirectory: "/this/path/does/not/exist/probably"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := workspaceSource(tc.task)
			if got.path != "" || got.kind != "" {
				t.Errorf("workspaceSource = %+v, want zero spec", got)
			}
		})
	}
}

func TestWorkspaceManager_InPlaceModeReturnsSourcePathWithoutCloning(t *testing.T) {
	// In-place mode skips the temp-dir clone — the workspace IS the
	// source. The sandbox AllowedRoot becomes the source path so
	// shell_exec / file / agent_loop tools can read and write the
	// operator's actual repo. Necessarily destructive, so opt-in.
	source := t.TempDir()
	// Drop a marker file so we can verify the manager didn't copy.
	marker := filepath.Join(source, "marker.txt")
	if err := os.WriteFile(marker, []byte("from-source"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	root := t.TempDir()
	mgr := NewWorkspaceManager(root)
	task := types.Task{ID: "task-1", WorkspaceMode: "in_place", WorkingDirectory: source}
	run := types.TaskRun{ID: "run-1"}

	got, err := mgr.Provision(context.Background(), task, run)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got != source {
		t.Errorf("workspace path = %q, want source %q (in_place must NOT clone)", got, source)
	}
	// And the temp root should NOT have a copy under task-1/run-1.
	cloned := filepath.Join(root, "task-1", "run-1")
	if _, err := os.Stat(cloned); !os.IsNotExist(err) {
		t.Errorf("expected no clone at %q, but it exists", cloned)
	}
}

func TestWorkspaceManager_InPlaceWithoutValidSourceFails(t *testing.T) {
	// in_place requires an absolute, existing source — silently
	// falling back to an isolated clone would be a surprising mode
	// flip. Reject up-front with a clear error.
	mgr := NewWorkspaceManager(t.TempDir())
	cases := []struct {
		name string
		task types.Task
	}{
		{"no working_directory", types.Task{ID: "t", WorkspaceMode: "in_place"}},
		{"relative path", types.Task{ID: "t", WorkspaceMode: "in_place", WorkingDirectory: "./nope"}},
		{"missing absolute path", types.Task{ID: "t", WorkspaceMode: "in_place", WorkingDirectory: "/this/does/not/exist/xyz"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mgr.Provision(context.Background(), tc.task, types.TaskRun{ID: "r"})
			if err == nil {
				t.Fatalf("expected error for in_place with %s", tc.name)
			}
			if !strings.Contains(err.Error(), "in_place") {
				t.Errorf("error = %q, want mention of in_place", err.Error())
			}
		})
	}
}

func TestWorkspaceManager_DefaultModeStillClones(t *testing.T) {
	// Default workspace mode (empty / persistent / ephemeral) must
	// keep the existing isolated-clone behavior so the safety
	// guarantee doesn't silently regress.
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "marker.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	root := t.TempDir()
	mgr := NewWorkspaceManager(root)
	task := types.Task{ID: "task-x", WorkingDirectory: source}
	run := types.TaskRun{ID: "run-x"}
	got, err := mgr.Provision(context.Background(), task, run)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	want := filepath.Join(root, "task-x", "run-x")
	if got != want {
		t.Errorf("workspace path = %q, want %q (cloned under temp root)", got, want)
	}
	// And the marker copied across.
	if _, err := os.Stat(filepath.Join(want, "marker.txt")); err != nil {
		t.Errorf("marker not copied to clone: %v", err)
	}
}

func TestWorkspaceSourceRejectsRegularFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got := workspaceSource(types.Task{WorkingDirectory: file})
	if got.path != "" || got.kind != "" {
		t.Errorf("workspaceSource(regular file) = %+v, want zero spec", got)
	}
}
