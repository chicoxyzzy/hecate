//go:build unix

package browserrunner

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type unixChromiumProcessTree struct{}

func prepareChromiumProcessTree(cmd *exec.Cmd) (chromiumProcessTree, error) {
	if cmd == nil {
		return nil, errors.New("chromium command is required")
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
	configureChromiumParentDeathSignal(cmd.SysProcAttr)
	tree := &unixChromiumProcessTree{}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return tree, nil
}

func (*unixChromiumProcessTree) attach(*exec.Cmd) error { return nil }

func (*unixChromiumProcessTree) forceKill(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (*unixChromiumProcessTree) close() {}
