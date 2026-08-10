//go:build !unix && !windows

package browserrunner

import (
	"errors"
	"os/exec"
)

func prepareChromiumProcessTree(*exec.Cmd) (chromiumProcessTree, error) {
	return nil, errors.New("Chromium process-tree supervision is unavailable on this platform")
}
