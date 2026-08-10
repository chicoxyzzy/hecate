//go:build linux

package browserrunner

import "syscall"

func configureChromiumParentDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
