//go:build unix && !linux

package browserrunner

import "syscall"

func configureChromiumParentDeathSignal(*syscall.SysProcAttr) {}
