package api

import (
	"log/slog"
	"strings"

	"github.com/hecatehq/hecate/internal/browserrunner"
	"github.com/hecatehq/hecate/internal/config"
)

// browserRuntimesFromConfig creates the native static-evidence and ephemeral
// interaction seams only when an operator explicitly configured a local
// executable. Returning the interface seams separately is load-bearing: a nil
// *ChromiumInspector converted by the caller would become a non-nil interface
// and incorrectly advertise unavailable browser tools. It also returns a
// path-free readiness record for the operator console; callers must never
// expose constructor errors because they can include a local filesystem path.
// The remote-runtime guard is repeated here so direct Handler construction in
// tests or embedders remains fail-closed even if Config.Validate was skipped.
func browserRuntimesFromConfig(cfg config.Config, logger *slog.Logger) (browserrunner.Inspector, browserrunner.FlowRunner, BrowserEvidenceRuntimeReadinessResponse) {
	if cfg.Server.RemoteRuntimeMode {
		return nil, nil, BrowserEvidenceRuntimeReadinessResponse{
			Status:         "local_only",
			Message:        "The native browser runtime is unavailable in remote runtime.",
			OperatorAction: "Run the task on a local Hecate runtime with browser capabilities configured.",
		}
	}
	if strings.TrimSpace(cfg.Server.TaskBrowserExecutable) == "" {
		return nil, nil, BrowserEvidenceRuntimeReadinessResponse{
			Status:         "not_configured",
			Message:        "The native browser runtime is not configured on this runtime.",
			OperatorAction: "Set HECATE_TASK_BROWSER_EXECUTABLE to an absolute path to a Chromium-compatible executable, then restart Hecate.",
		}
	}
	inspector, err := browserrunner.New(browserrunner.Config{
		ExecutablePath:  cfg.Server.TaskBrowserExecutable,
		Timeout:         cfg.Server.TaskBrowserTimeout,
		AllowPrivateIPs: cfg.Server.TaskBrowserAllowPrivateIPs,
	})
	if err != nil {
		if logger != nil {
			// Do not attach err: it may contain a local filesystem path.
			logger.Warn("native browser runtime is unavailable; browser tools will be omitted")
		}
		return nil, nil, BrowserEvidenceRuntimeReadinessResponse{
			Status:         "unavailable",
			Message:        "The native browser runtime is unavailable from the current runtime configuration.",
			OperatorAction: "Check HECATE_TASK_BROWSER_EXECUTABLE and its executable permissions, then restart Hecate.",
		}
	}
	return inspector, inspector, BrowserEvidenceRuntimeReadinessResponse{
		Available: true,
		Status:    "ready",
		Message:   "The native browser runtime is ready on this local runtime for static evidence and approved interaction.",
	}
}
