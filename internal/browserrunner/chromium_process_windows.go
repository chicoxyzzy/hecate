//go:build windows

package browserrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsChromiumProcessTree struct {
	mu       sync.Mutex
	job      windows.Handle
	attached bool
}

// prepareChromiumProcessTree starts Chromium suspended, assigns it to a
// kill-on-close Job Object, and only then lets Chromium create renderer or
// network descendants. This closes the child-escape window between Start and
// Job assignment.
func prepareChromiumProcessTree(cmd *exec.Cmd) (chromiumProcessTree, error) {
	if cmd == nil {
		return nil, errors.New("chromium command is required")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	tree := &windowsChromiumProcessTree{job: job}
	cmd.Cancel = func() error {
		return tree.forceKill(cmd)
	}
	return tree, nil
}

func (t *windowsChromiumProcessTree) attach(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return errors.New("chromium process is unavailable")
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(t.job, process); err != nil {
		return err
	}
	t.mu.Lock()
	t.attached = true
	t.mu.Unlock()
	if err := resumeChromiumProcess(uint32(cmd.Process.Pid)); err != nil {
		return fmt.Errorf("resume suspended Chromium process: %w", err)
	}
	return nil
}

func resumeChromiumProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return err
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			_, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			return resumeErr
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return fmt.Errorf("primary thread for Chromium process %d not found", pid)
			}
			return err
		}
	}
}

func (t *windowsChromiumProcessTree) forceKill(cmd *exec.Cmd) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.attached {
		if cmd == nil || cmd.Process == nil {
			return nil
		}
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		return nil
	}
	if t.job == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(t.job, 1); err != nil && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	return nil
}

func (t *windowsChromiumProcessTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}
