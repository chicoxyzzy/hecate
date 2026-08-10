package browserrunner

import "os/exec"

// chromiumProcessTree keeps the browser leader unreaped until every owned
// renderer/network descendant has been terminated. Implementations use a Unix
// process group or a Windows kill-on-close Job Object.
type chromiumProcessTree interface {
	attach(*exec.Cmd) error
	forceKill(*exec.Cmd) error
	close()
}
