//go:build windows

package browserrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const chromiumProcessTreeHelperMode = "HECATE_CHROMIUM_PROCESS_TREE_HELPER"
const chromiumProcessTreePIDFile = "HECATE_CHROMIUM_PROCESS_TREE_PID_FILE"

func TestChromiumProcessTreeCancellationTerminatesDescendants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChromiumProcessTreeHelperProcess$")
	cmd.Env = append(os.Environ(),
		chromiumProcessTreeHelperMode+"=parent",
		chromiumProcessTreePIDFile+"="+pidFile,
	)
	tree, err := prepareChromiumProcessTree(cmd)
	if err != nil {
		cancel()
		t.Fatalf("prepare Chromium process tree: %v", err)
	}
	defer tree.close()
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start helper parent: %v", err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = tree.forceKill(cmd)
			cancel()
			_ = cmd.Wait()
		}
	}()
	if err := tree.attach(cmd); err != nil {
		t.Fatalf("attach helper parent: %v", err)
	}
	childPID := waitForChromiumProcessTreeChildPID(t, pidFile)
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(childPID))
	if err != nil {
		t.Fatalf("open helper child %d: %v", childPID, err)
	}
	defer windows.CloseHandle(child)

	cancel()
	_ = cmd.Wait()
	waited = true
	result, err := windows.WaitForSingleObject(child, 5_000)
	if err != nil {
		t.Fatalf("wait for helper child: %v", err)
	}
	if result != windows.WAIT_OBJECT_0 {
		t.Fatalf("helper child %d survived Chromium Job Object termination (wait result %#x)", childPID, result)
	}
}

func TestChromiumProcessTreeHelperProcess(t *testing.T) {
	mode := strings.TrimSpace(os.Getenv(chromiumProcessTreeHelperMode))
	if mode == "" {
		return
	}
	if mode == "parent" {
		child := exec.Command(os.Args[0], "-test.run=^TestChromiumProcessTreeHelperProcess$")
		child.Env = append(os.Environ(), chromiumProcessTreeHelperMode+"=child")
		if err := child.Start(); err != nil {
			panic(fmt.Sprintf("start helper child: %v", err))
		}
		pidFile := os.Getenv(chromiumProcessTreePIDFile)
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			panic(fmt.Sprintf("write helper child pid: %v", err))
		}
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitForChromiumProcessTreeChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr != nil || pid <= 0 {
				t.Fatalf("parse helper child pid %q: %v", raw, parseErr)
			}
			return pid
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("helper child pid was not written to %s", path)
	return 0
}
