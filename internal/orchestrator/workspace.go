package orchestrator

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hecate/agent-runtime/pkg/types"
)

type WorkspaceManager struct {
	root string
}

func NewWorkspaceManager(root string) *WorkspaceManager {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(os.TempDir(), "hecate-workspaces")
	}
	return &WorkspaceManager{root: root}
}

func (m *WorkspaceManager) Provision(ctx context.Context, task types.Task, run types.TaskRun) (string, error) {
	if m == nil {
		return "", fmt.Errorf("workspace manager is not configured")
	}
	source := workspaceSource(task)
	// "in_place" mode: skip the clone/copy and run directly in the
	// source directory. The sandbox AllowedRoot becomes the source
	// path, so writes from shell_exec/file/agent_loop tools land in
	// the operator's actual repo. Necessarily destructive — opt-in
	// only. We require an absolute, existing source directory; if
	// task.WorkingDirectory or task.Repo doesn't resolve to one, we
	// reject the run rather than silently fall back to an isolated
	// clone (which would be a surprising mode flip).
	if strings.TrimSpace(task.WorkspaceMode) == "in_place" {
		if source.path == "" {
			return "", fmt.Errorf("workspace_mode=in_place requires an absolute, existing working_directory or repo path")
		}
		return source.path, nil
	}
	workspacePath := filepath.Join(m.root, task.ID, run.ID)
	if err := provisionWorkspaceSource(ctx, workspacePath, source); err != nil {
		return "", err
	}
	return workspacePath, nil
}

type workspaceSourceSpec struct {
	path string
	kind string
}

func workspaceSource(task types.Task) workspaceSourceSpec {
	for _, candidate := range []string{task.WorkingDirectory, task.Repo} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		if isGitRepository(candidate) {
			return workspaceSourceSpec{path: candidate, kind: "git"}
		}
		return workspaceSourceSpec{path: candidate, kind: "directory"}
	}
	return workspaceSourceSpec{}
}

func provisionWorkspaceSource(ctx context.Context, workspacePath string, source workspaceSourceSpec) error {
	switch source.kind {
	case "git":
		return provisionGitWorkspace(ctx, source.path, workspacePath)
	case "directory":
		return provisionDirectoryWorkspace(source.path, workspacePath)
	default:
		return ensureWorkspaceRoot(workspacePath)
	}
}

func provisionGitWorkspace(ctx context.Context, sourcePath, workspacePath string) error {
	if err := ensureWorkspaceParent(workspacePath); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", sourcePath, workspacePath).CombinedOutput(); err != nil {
		return fmt.Errorf("clone workspace: %w: %s", err, string(output))
	}
	return nil
}

func provisionDirectoryWorkspace(sourcePath, workspacePath string) error {
	if err := ensureWorkspaceRoot(workspacePath); err != nil {
		return err
	}
	return copyDirectory(sourcePath, workspacePath)
}

func ensureWorkspaceParent(workspacePath string) error {
	return os.MkdirAll(filepath.Dir(workspacePath), 0o755)
}

func ensureWorkspaceRoot(workspacePath string) error {
	return os.MkdirAll(workspacePath, 0o755)
}

func isGitRepository(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

func copyDirectory(sourcePath, destinationPath string) error {
	return filepath.WalkDir(sourcePath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sourcePath {
			return nil
		}

		relativePath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destinationPath, relativePath)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, targetPath)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return copyFile(path, targetPath, info.Mode())
	})
}

func copyFile(sourcePath, destinationPath string, mode fs.FileMode) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	targetFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, sourceFile)
	return err
}
