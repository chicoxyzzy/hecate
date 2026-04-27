package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGateway spins up an in-process HTTP server with the routes the
// MCP tools call. Test bodies install handlers per-route via the
// handlers map; unmocked routes 404. The fake checks Authorization on
// every request so tests cover the bearer-token contract too.
func fakeGateway(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	const token = "test-token"
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, token
}

func TestTool_ListTasks_FormatsRows(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/tasks": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("limit") != "5" {
				t.Errorf("limit query = %q, want 5", r.URL.Query().Get("limit"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"id":"task-abc12345","title":"List wd","status":"completed","execution_kind":"shell","step_count":2,"latest_run_id":"run-fedc0987"},
				{"id":"task-xyz98765","title":"","prompt":"echo hi","status":"running","execution_kind":"shell","step_count":1}
			]}`))
		},
	})
	client := NewHTTPClient(srv.URL, token)
	server := NewServer("hecate-test", "0.0.0")
	RegisterDefaultTools(server, client)

	args := json.RawMessage(`{"limit":5}`)
	handler := registeredToolFor(t, server, "list_tasks")
	result, err := handler(context.Background(), args)
	if err != nil {
		t.Fatalf("list_tasks: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("no content blocks")
	}
	body := result.Content[0].Text
	if !strings.Contains(body, "Found 2 task(s)") {
		t.Errorf("want count header, got: %s", body)
	}
	if !strings.Contains(body, "task-abc") {
		t.Errorf("want short id task-abc, got: %s", body)
	}
	if !strings.Contains(body, "List wd") {
		t.Errorf("want title 'List wd', got: %s", body)
	}
	// Empty title falls back to prompt.
	if !strings.Contains(body, "echo hi") {
		t.Errorf("want prompt fallback 'echo hi', got: %s", body)
	}
}

func TestTool_ListTasks_EmptyState(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/tasks": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		},
	})
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient(srv.URL, token))
	result, err := registeredToolFor(t, server, "list_tasks")(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "No tasks yet") {
		t.Errorf("want empty state, got: %s", result.Content[0].Text)
	}
}

func TestTool_GetTaskStatus_RequiresID(t *testing.T) {
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient("http://unused", ""))
	_, err := registeredToolFor(t, server, "get_task_status")(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("want task_id required error, got: %v", err)
	}
}

func TestTool_GetTaskStatus_FormatsDetail(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/tasks/abc-123": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"id":"abc-123","title":"Run db migration","status":"completed","execution_kind":"shell","shell_command":"./migrate.sh","step_count":3,"latest_run_id":"run-1","created_at":"2026-04-22T10:00:00Z"}}`))
		},
	})
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient(srv.URL, token))
	result, err := registeredToolFor(t, server, "get_task_status")(context.Background(),
		json.RawMessage(`{"task_id":"abc-123"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body := result.Content[0].Text
	for _, want := range []string{"abc-123", "Run db migration", "completed", "shell", "./migrate.sh", "Steps: 3"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, got: %s", want, body)
		}
	}
}

func TestTool_ListChatSessions_TenantFilter(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/chat/sessions": func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("tenant"); got != "team-a" {
				t.Errorf("tenant query = %q, want team-a", got)
			}
			if r.URL.Query().Get("limit") != "5" {
				t.Errorf("limit query = %q, want 5", r.URL.Query().Get("limit"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"sess-1","title":"Hello","tenant":"team-a","turn_count":3,"updated_at":"2026-04-22T10:00:00Z"}]}`))
		},
	})
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient(srv.URL, token))
	result, err := registeredToolFor(t, server, "list_chat_sessions")(context.Background(),
		json.RawMessage(`{"limit":5,"tenant":"team-a"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body := result.Content[0].Text
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "team-a") || !strings.Contains(body, "(3 turns)") {
		t.Errorf("body missing fields: %s", body)
	}
}

func TestTool_SummarizeTraffic_AggregatesByProvider(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/traces": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"request_id":"r1","provider":"openai","duration_ms":120,"status_code":"ok","total_tokens":150,"cost_usd":0.001},
				{"request_id":"r2","provider":"openai","duration_ms":80,"status_code":"ok","total_tokens":100,"cost_usd":0.0008},
				{"request_id":"r3","provider":"anthropic","duration_ms":300,"status_code":"error"}
			]}`))
		},
	})
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient(srv.URL, token))
	result, err := registeredToolFor(t, server, "summarize_recent_traffic")(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body := result.Content[0].Text
	for _, want := range []string{"3 requests", "openai: 2 req", "anthropic: 1 req", "1 errors"} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in output, got: %s", want, body)
		}
	}
}

func TestTool_UpstreamError_BubblesAsToolError(t *testing.T) {
	srv, token := fakeGateway(t, map[string]http.HandlerFunc{
		"/v1/tasks": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal", http.StatusInternalServerError)
		},
	})
	server := NewServer("t", "0")
	RegisterDefaultTools(server, NewHTTPClient(srv.URL, token))
	_, err := registeredToolFor(t, server, "list_tasks")(context.Background(), json.RawMessage(`{}`))
	// Tool returns the error directly; the dispatcher wraps it in
	// CallToolResult.IsError=true, but at this seam we just see the
	// error value.
	if err == nil {
		t.Fatal("expected upstream error to propagate")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("want status code in error, got: %v", err)
	}
}

// registeredToolFor pulls a registered tool's handler out of the
// server. Tests would otherwise need to drive everything through the
// stdio loop, which is excessive for tool-level assertions.
func registeredToolFor(t *testing.T, s *Server, name string) ToolHandler {
	t.Helper()
	tool, ok := s.tools.byName[name]
	if !ok {
		t.Fatalf("tool %q not registered; have: %+v", name, toolNames(s))
	}
	return tool.handler
}

func toolNames(s *Server) []string {
	out := make([]string, 0, len(s.tools.byName))
	for n := range s.tools.byName {
		out = append(out, n)
	}
	return out
}
