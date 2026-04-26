package orchestrator

import (
	"os"
	"path/filepath"
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
