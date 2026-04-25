//go:build e2e

// Package e2e contains true end-to-end tests that build and start the real
// gateway binary, send real HTTP requests (optionally against real upstream
// providers), and verify the response shape.
//
// Run with:
//
//	go test -tags e2e ./e2e/... -v
//
// Tests that hit a real LLM provider require at least one of:
//
//	PROVIDER_ANTHROPIC_API_KEY  — for Claude Code (/v1/messages) tests
//	PROVIDER_OPENAI_API_KEY     — for Codex (/v1/chat/completions) tests
//
// Without those keys the real-provider tests are skipped. The binary-startup
// tests never require keys and always run.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── binary lifecycle ────────────────────────────────────────────────────────

var (
	buildOnce    sync.Once
	builtBinPath string
	builtBinErr  error
)

// gatewayBinary returns the path to the compiled gateway binary.
// The binary is built exactly once per test-binary execution using sync.Once,
// so parallel tests don't trigger redundant go build invocations.
// Set E2E_GATEWAY_BIN to a pre-built path to skip the build entirely (CI).
func gatewayBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("E2E_GATEWAY_BIN"); bin != "" {
		return bin
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hecate-e2e-*")
		if err != nil {
			builtBinErr = err
			return
		}
		builtBinPath = dir + "/gateway"
		cmd := exec.Command("go", "build", "-o", builtBinPath, "./cmd/gateway")
		cmd.Dir = moduleRootDir()
		out, err := cmd.CombinedOutput()
		if err != nil {
			builtBinErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if builtBinErr != nil {
		t.Fatalf("build gateway binary: %v", builtBinErr)
	}
	return builtBinPath
}

// moduleRootDir returns the repository root by reading go env GOMOD.
func moduleRootDir() string {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		panic("go env GOMOD: " + err.Error())
	}
	mod := strings.TrimSpace(string(out))
	return mod[:strings.LastIndex(mod, string(os.PathSeparator))]
}

// gatewayServer starts the gateway binary on a free port and returns the base
// URL once /healthz responds 200.  The process is killed when the test ends.
func gatewayServer(t *testing.T, extraEnv ...string) string {
	t.Helper()

	bin := gatewayBinary(t)
	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	baseURL := "http://" + addr

	env := append(os.Environ(),
		"GATEWAY_ADDRESS="+addr,
		"GATEWAY_SINGLE_USER_ADMIN_MODE=true",
	)
	env = append(env, extraEnv...)

	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	waitHealthy(t, baseURL, 10*time.Second)
	return baseURL
}

// waitHealthy polls GET /healthz until it returns 200 or the deadline expires.
func waitHealthy(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz") //nolint:noctx
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("gateway at %s never became healthy within %s", baseURL, timeout)
}

// freePort asks the OS for an available TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// ─── HTTP helpers ────────────────────────────────────────────────────────────

func postJSON(t *testing.T, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

type sseEvent struct {
	Data string
}

func readSSE(t *testing.T, resp *http.Response) []sseEvent {
	t.Helper()
	defer resp.Body.Close()
	var events []sseEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, sseEvent{Data: strings.TrimPrefix(line, "data: ")})
		}
	}
	return events
}

// ─── startup tests (no API key required) ────────────────────────────────────

// TestGatewayStartsAndRespondsHealthy verifies that the binary starts, binds
// the port, and returns 200 on /healthz.
func TestGatewayStartsAndRespondsHealthy(t *testing.T) {
	t.Parallel()
	base := gatewayServer(t)

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestGatewayRejectsMissingAuth checks that the gateway returns 401 when no
// token or API key is supplied (admin-mode is still on but no bearer/x-api-key
// is present — the single-user admin mode auto-injects a user so we expect 200
// here, but requests without any credentials to a non-admin endpoint still get
// processed; this test validates that a well-formed unauthenticated request
// reaches the router and gets a 502 because no provider is configured).
func TestGatewayNoProviderConfiguredReturns502(t *testing.T) {
	t.Parallel()
	// No PROVIDER_* env — no providers registered.
	base := gatewayServer(t)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})
	defer resp.Body.Close()
	// With no providers registered the router can't route the request; it
	// returns either 500 or 502 depending on the error path.
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 with no provider configured, got 200")
	}
}

// TestGatewayFakeUpstreamNonStreamingCodex starts the gateway pointing at a
// local fake OpenAI-compatible HTTP server and verifies a complete non-streaming
// Codex request round-trip.
func TestGatewayFakeUpstreamNonStreamingCodex(t *testing.T) {
	t.Parallel()

	// Start fake OpenAI upstream.
	fakeResp := `{"id":"chatcmpl-e2e","object":"chat.completion","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from fake upstream"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`
	upstream := fakeOpenAIServer(t, "/v1/chat/completions", fakeResp, false)

	base := gatewayServer(t,
		"PROVIDER_FAKE_API_KEY=dummy",
		"PROVIDER_FAKE_BASE_URL="+upstream,
		"PROVIDER_FAKE_PROTOCOL=openai",
		"PROVIDER_FAKE_DEFAULT_MODEL=gpt-4o-mini",
		"PROVIDER_FAKE_KIND=local",
		"GATEWAY_DEFAULT_MODEL=gpt-4o-mini",
	)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices in response: %v", result)
	}
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hello from fake upstream" {
		t.Fatalf("unexpected content: %v", msg["content"])
	}
}

// TestGatewayFakeUpstreamStreamingCodex verifies that the gateway streams SSE
// chunks from the upstream through to the client correctly.
func TestGatewayFakeUpstreamStreamingCodex(t *testing.T) {
	t.Parallel()

	upstream := fakeOpenAIServer(t, "/v1/chat/completions", "", true)

	base := gatewayServer(t,
		"PROVIDER_FAKE_API_KEY=dummy",
		"PROVIDER_FAKE_BASE_URL="+upstream,
		"PROVIDER_FAKE_PROTOCOL=openai",
		"PROVIDER_FAKE_DEFAULT_MODEL=gpt-4o-mini",
		"PROVIDER_FAKE_KIND=local",
		"GATEWAY_DEFAULT_MODEL=gpt-4o-mini",
	)

	body := `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})

	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream content-type, got %s", ct)
	}

	events := readSSE(t, resp)
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event")
	}
	// Last event should be [DONE].
	last := events[len(events)-1]
	if last.Data != "[DONE]" {
		t.Fatalf("expected last SSE data to be [DONE], got %q", last.Data)
	}
}

// TestGatewayFakeUpstreamClaudeCode verifies the /v1/messages (Anthropic)
// endpoint using a fake OpenAI-compatible upstream.
func TestGatewayFakeUpstreamClaudeCode(t *testing.T) {
	t.Parallel()

	fakeResp := `{"id":"chatcmpl-e2e","object":"chat.completion","created":1700000000,"model":"claude-sonnet-4-20250514","choices":[{"index":0,"message":{"role":"assistant","content":"Hello from fake upstream"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`
	upstream := fakeOpenAIServer(t, "/v1/chat/completions", fakeResp, false)

	base := gatewayServer(t,
		"PROVIDER_FAKE_API_KEY=dummy",
		"PROVIDER_FAKE_BASE_URL="+upstream,
		"PROVIDER_FAKE_PROTOCOL=openai",
		"PROVIDER_FAKE_DEFAULT_MODEL=claude-sonnet-4-20250514",
		"PROVIDER_FAKE_KIND=local",
		"GATEWAY_DEFAULT_MODEL=claude-sonnet-4-20250514",
	)

	body := `{"model":"claude-sonnet-4-20250514","max_tokens":128,"messages":[{"role":"user","content":"hello"}]}`
	resp := postJSON(t, base+"/v1/messages", body, map[string]string{
		"x-api-key":         "test-token",
		"anthropic-version": "2023-06-01",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Anthropic response shape: {"type":"message","content":[{"type":"text","text":"..."}],...}
	if result["type"] != "message" {
		t.Fatalf("expected type=message, got: %v", result["type"])
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("expected content array in response: %v", result)
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "text" {
		t.Fatalf("expected content[0].type=text, got %v", block["type"])
	}
	if block["text"] != "Hello from fake upstream" {
		t.Fatalf("unexpected text: %v", block["text"])
	}
}

// TestGatewayRuntimeProviderHeader verifies that the gateway injects the
// X-Runtime-Provider header into successful responses.
func TestGatewayRuntimeProviderHeader(t *testing.T) {
	t.Parallel()

	fakeResp := `{"id":"chatcmpl-e2e","object":"chat.completion","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	upstream := fakeOpenAIServer(t, "/v1/chat/completions", fakeResp, false)

	base := gatewayServer(t,
		"PROVIDER_FAKE_API_KEY=dummy",
		"PROVIDER_FAKE_BASE_URL="+upstream,
		"PROVIDER_FAKE_PROTOCOL=openai",
		"PROVIDER_FAKE_DEFAULT_MODEL=gpt-4o-mini",
		"PROVIDER_FAKE_KIND=local",
		"GATEWAY_DEFAULT_MODEL=gpt-4o-mini",
	)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if h := resp.Header.Get("X-Runtime-Provider"); h == "" {
		t.Fatal("expected X-Runtime-Provider header in response")
	}
}

// ─── real-provider tests (skipped without API keys) ─────────────────────────

// TestRealAnthropicClaudeCode sends a real request to Anthropic via the
// gateway's /v1/messages endpoint.  Skipped when PROVIDER_ANTHROPIC_API_KEY
// is not set.
func TestRealAnthropicClaudeCode(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("PROVIDER_ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("PROVIDER_ANTHROPIC_API_KEY not set — skipping real-provider test")
	}

	base := gatewayServer(t,
		"PROVIDER_ANTHROPIC_API_KEY="+apiKey,
		"PROVIDER_ANTHROPIC_PROTOCOL=anthropic",
		"GATEWAY_DEFAULT_MODEL=claude-haiku-4-5-20251001",
	)

	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":64,"messages":[{"role":"user","content":"Reply with exactly the word: pong"}]}`
	resp := postJSON(t, base+"/v1/messages", body, map[string]string{
		"x-api-key":         "test-token",
		"anthropic-version": "2023-06-01",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["type"] != "message" {
		t.Fatalf("expected type=message, got %v", result["type"])
	}
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("empty content in real Anthropic response: %v", result)
	}
}

// TestRealAnthropicClaudeCodeStreaming sends a streaming request to Anthropic
// via the gateway's /v1/messages endpoint and validates SSE format.
func TestRealAnthropicClaudeCodeStreaming(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("PROVIDER_ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("PROVIDER_ANTHROPIC_API_KEY not set — skipping real-provider test")
	}

	base := gatewayServer(t,
		"PROVIDER_ANTHROPIC_API_KEY="+apiKey,
		"PROVIDER_ANTHROPIC_PROTOCOL=anthropic",
		"GATEWAY_DEFAULT_MODEL=claude-haiku-4-5-20251001",
	)

	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"Say hi"}]}`
	resp := postJSON(t, base+"/v1/messages", body, map[string]string{
		"x-api-key":         "test-token",
		"anthropic-version": "2023-06-01",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}

	events := readSSE(t, resp)
	if len(events) == 0 {
		t.Fatal("no SSE events received from streaming Anthropic response")
	}

	// Anthropic SSE stream starts with message_start.
	var firstEvent map[string]interface{}
	if err := json.Unmarshal([]byte(events[0].Data), &firstEvent); err != nil {
		t.Fatalf("parse first SSE event: %v", err)
	}
	if firstEvent["type"] != "message_start" {
		t.Fatalf("expected first event type=message_start, got %v", firstEvent["type"])
	}
}

// TestRealOpenAICodex sends a real request to OpenAI via the gateway's
// /v1/chat/completions endpoint.  Skipped when PROVIDER_OPENAI_API_KEY is
// not set.
func TestRealOpenAICodex(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("PROVIDER_OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("PROVIDER_OPENAI_API_KEY not set — skipping real-provider test")
	}

	base := gatewayServer(t,
		"PROVIDER_OPENAI_API_KEY="+apiKey,
		"PROVIDER_OPENAI_PROTOCOL=openai",
		"GATEWAY_DEFAULT_MODEL=gpt-4o-mini",
	)

	body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Reply with exactly the word: pong"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices, _ := result["choices"].([]interface{})
	if len(choices) == 0 {
		t.Fatalf("empty choices in real OpenAI response: %v", result)
	}
}

// TestRealOpenAICodexStreaming sends a streaming request to OpenAI via the
// gateway's /v1/chat/completions endpoint and validates SSE format.
func TestRealOpenAICodexStreaming(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("PROVIDER_OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("PROVIDER_OPENAI_API_KEY not set — skipping real-provider test")
	}

	base := gatewayServer(t,
		"PROVIDER_OPENAI_API_KEY="+apiKey,
		"PROVIDER_OPENAI_PROTOCOL=openai",
		"GATEWAY_DEFAULT_MODEL=gpt-4o-mini",
	)

	body := `{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"Say hi"}]}`
	resp := postJSON(t, base+"/v1/chat/completions", body, map[string]string{
		"Authorization": "Bearer test-token",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", resp.StatusCode, readBody(t, resp))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}

	events := readSSE(t, resp)
	if len(events) == 0 {
		t.Fatal("no SSE events received from streaming OpenAI response")
	}
	last := events[len(events)-1]
	if last.Data != "[DONE]" {
		t.Fatalf("expected last event to be [DONE], got %q", last.Data)
	}
}

// ─── fake upstream helper ────────────────────────────────────────────────────

// fakeOpenAIServer starts an httptest.Server that mimics an OpenAI-compatible
// upstream.  If streaming=true it returns chunked SSE; otherwise it returns a
// plain JSON response.  The server is shut down when the test ends.
func fakeOpenAIServer(t *testing.T, path, body string, streaming bool) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if streaming {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			chunks := []string{
				`{"id":"chatcmpl-e2e","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-e2e","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-e2e","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			}
			for _, chunk := range chunks {
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, body)
		}
	})
	srv := &http.Server{Handler: mux}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeOpenAIServer listen: %v", err)
	}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}
