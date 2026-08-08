# Cross-cutting smoke tests (Docker compose round-trip). Tauri's GUI smoke
# lives in tauri.just; ACP and other tagged Go smokes live alongside their
# subjects in go.just / e2e/.

# Spin up `docker compose` with the production image and verify /healthz,
# /v1/models auth, and the bootstrap volume round trip. Runs against a
# separate compose project name so it cannot collide with a developer's
# already-running `docker compose up`. Requires Docker.
# Docker smoke test for the production compose image.
test-docker-smoke: _go-cache
	{{go_env}} go test -tags 'e2e docker' -count=1 -timeout 10m ./e2e/...

# Exercise the built-in Go adapters with authenticated local provider CLIs.
# Successful prompts may consume operator quota, so this stays opt-in.
# Smoke test authenticated built-in adapter vendor CLIs.
test-acp-real-embedded adapters='codex,claude_code': _go-cache
	HECATE_ACP_REAL_CLI_SMOKE=1 HECATE_ACP_REAL_ADAPTERS='{{adapters}}' {{go_env}} go test -tags real_cli -count=1 -run TestRealACPCLIsSmoke -timeout 10m ./internal/agentadapters

# Exercise Cursor and Grok's direct ACP modes with authenticated local CLIs.
# Successful prompts may consume operator quota, so this stays opt-in.
# Smoke test authenticated direct ACP vendor CLIs.
test-acp-real-direct adapters='cursor_agent,grok_build': _go-cache
	HECATE_ACP_REAL_CLI_SMOKE=1 HECATE_ACP_REAL_ADAPTERS='{{adapters}}' {{go_env}} go test -tags real_cli -count=1 -run TestRealACPCLIsSmoke -timeout 10m ./internal/agentadapters

# Run the real Chromium transport, isolation, and accessibility-action matrix.
# This is opt-in because ordinary contributors may not have a compatible local
# browser. CI discovers its installed browser and sets the executable explicitly.
# Smoke test the native browser security boundary with a real Chromium runtime.
test-browser-smoke: _go-cache
	@test -n "${HECATE_TASK_BROWSER_EXECUTABLE:-}" || { echo "HECATE_TASK_BROWSER_EXECUTABLE must name an absolute Chromium-compatible executable" >&2; exit 2; }
	HECATE_BROWSER_SMOKE=1 {{go_env}} go test -count=1 -timeout 10m ./internal/browserrunner

# Exercise model tool selection through real Project assignments. This is
# opt-in because it uses an operator-configured provider and may consume quota.
# Run the code-intelligence model dogfood scorecard.
test-codeintel-dogfood: _go-cache
	E2E_HECATE_BIN= HECATE_CODEINTEL_AGENT_DOGFOOD=1 {{go_env}} go test -tags e2e -count=1 -run '^TestCodeIntelligenceModelDogfood$' -timeout 3h -v ./e2e
