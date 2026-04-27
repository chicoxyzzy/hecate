package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// RegisterDefaultTools wires the v0.1 tool set onto the supplied
// server. Every tool here dispatches through the HTTPClient — the MCP
// server is a translator, not a re-implementation of Hecate's core.
//
// Tool design conventions:
//   - inputSchema is JSON Schema 2020-12 (what MCP clients expect).
//     Properties are typed and described; required fields are marked.
//   - Tool output is always plain text (one block) for v0.1. Rich
//     content (markdown tables, JSON dumps) is rendered as text the
//     client formats. We may switch to structured `resource` blocks
//     once clients render them better.
//   - Errors that originate at the upstream HTTP layer become the
//     handler's error return → CallToolResult with isError=true.
func RegisterDefaultTools(s *Server, client *HTTPClient) {
	s.RegisterTool(Tool{
		Name:        "list_tasks",
		Description: "List recent agent tasks tracked by the Hecate gateway. Returns each task's id, title, status, and execution kind.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 30, "description": "Maximum number of tasks to return."}
			}
		}`),
	}, listTasksHandler(client))

	s.RegisterTool(Tool{
		Name:        "get_task_status",
		Description: "Get the current status of a specific Hecate task by id, including its latest run and step count.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task_id": {"type": "string", "description": "The task id (UUID-shaped)."}
			},
			"required": ["task_id"]
		}`),
	}, getTaskStatusHandler(client))

	s.RegisterTool(Tool{
		Name:        "list_chat_sessions",
		Description: "List recent chat sessions on the Hecate gateway. Returns each session's id, title, turn count, and last-updated time.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20, "description": "Maximum number of sessions to return."},
				"tenant": {"type": "string", "description": "Filter to a single tenant id. Empty = all tenants the caller can see."}
			}
		}`),
	}, listChatSessionsHandler(client))

	s.RegisterTool(Tool{
		Name:        "summarize_recent_traffic",
		Description: "Summarize recent gateway request activity: total count, by-provider breakdown, error rate, and average latency.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"limit": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100, "description": "Number of recent traces to inspect."}
			}
		}`),
	}, summarizeRecentTrafficHandler(client))
}

// ─── list_tasks ─────────────────────────────────────────────────────

type listTasksArgs struct {
	Limit int `json:"limit"`
}

type listTasksResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		Prompt        string `json:"prompt"`
		Status        string `json:"status"`
		ExecutionKind string `json:"execution_kind"`
		StepCount     int    `json:"step_count"`
		LatestRunID   string `json:"latest_run_id"`
		CreatedAt     string `json:"created_at"`
	} `json:"data"`
}

func listTasksHandler(client *HTTPClient) ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (CallToolResult, error) {
		var args listTasksArgs
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &args)
		}
		if args.Limit <= 0 {
			args.Limit = 30
		}
		q := url.Values{}
		q.Set("limit", fmt.Sprintf("%d", args.Limit))

		var resp listTasksResponse
		if err := client.Get(ctx, "/v1/tasks", q, &resp); err != nil {
			return CallToolResult{}, err
		}
		if len(resp.Data) == 0 {
			return CallToolResult{Content: TextContent("No tasks yet.")}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Found %d task(s):\n\n", len(resp.Data))
		for _, t := range resp.Data {
			title := t.Title
			if title == "" {
				title = t.Prompt
			}
			fmt.Fprintf(&b, "- %s [%s] %s — %s (%d steps)",
				shortID(t.ID), t.ExecutionKind, t.Status, title, t.StepCount)
			if t.LatestRunID != "" {
				fmt.Fprintf(&b, " · run %s", shortID(t.LatestRunID))
			}
			b.WriteByte('\n')
		}
		return CallToolResult{Content: TextContent(b.String())}, nil
	}
}

// ─── get_task_status ────────────────────────────────────────────────

type getTaskStatusArgs struct {
	TaskID string `json:"task_id"`
}

type getTaskStatusResponse struct {
	Data struct {
		ID            string `json:"id"`
		Title         string `json:"title"`
		Prompt        string `json:"prompt"`
		Status        string `json:"status"`
		ExecutionKind string `json:"execution_kind"`
		ShellCommand  string `json:"shell_command,omitempty"`
		GitCommand    string `json:"git_command,omitempty"`
		FilePath      string `json:"file_path,omitempty"`
		StepCount     int    `json:"step_count"`
		LatestRunID   string `json:"latest_run_id"`
		CreatedAt     string `json:"created_at"`
		UpdatedAt     string `json:"updated_at"`
	} `json:"data"`
}

func getTaskStatusHandler(client *HTTPClient) ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (CallToolResult, error) {
		var args getTaskStatusArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return CallToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(args.TaskID) == "" {
			return CallToolResult{}, fmt.Errorf("task_id is required")
		}
		var resp getTaskStatusResponse
		if err := client.Get(ctx, "/v1/tasks/"+url.PathEscape(args.TaskID), nil, &resp); err != nil {
			return CallToolResult{}, err
		}
		t := resp.Data
		var b strings.Builder
		fmt.Fprintf(&b, "Task %s\n", t.ID)
		if t.Title != "" {
			fmt.Fprintf(&b, "Title: %s\n", t.Title)
		}
		fmt.Fprintf(&b, "Status: %s\n", t.Status)
		fmt.Fprintf(&b, "Kind: %s\n", t.ExecutionKind)
		switch t.ExecutionKind {
		case "shell":
			if t.ShellCommand != "" {
				fmt.Fprintf(&b, "Command: %s\n", t.ShellCommand)
			}
		case "git":
			if t.GitCommand != "" {
				fmt.Fprintf(&b, "Command: git %s\n", t.GitCommand)
			}
		case "file":
			if t.FilePath != "" {
				fmt.Fprintf(&b, "File: %s\n", t.FilePath)
			}
		}
		fmt.Fprintf(&b, "Steps: %d\n", t.StepCount)
		if t.LatestRunID != "" {
			fmt.Fprintf(&b, "Latest run: %s\n", t.LatestRunID)
		}
		if t.CreatedAt != "" {
			fmt.Fprintf(&b, "Created: %s\n", t.CreatedAt)
		}
		if t.UpdatedAt != "" {
			fmt.Fprintf(&b, "Updated: %s\n", t.UpdatedAt)
		}
		return CallToolResult{Content: TextContent(b.String())}, nil
	}
}

// ─── list_chat_sessions ──────────────────────────────────────────────

type listChatSessionsArgs struct {
	Limit  int    `json:"limit"`
	Tenant string `json:"tenant"`
}

type listChatSessionsResponse struct {
	Data []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Tenant    string `json:"tenant"`
		TurnCount int    `json:"turn_count"`
		UpdatedAt string `json:"updated_at"`
	} `json:"data"`
}

func listChatSessionsHandler(client *HTTPClient) ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (CallToolResult, error) {
		var args listChatSessionsArgs
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &args)
		}
		if args.Limit <= 0 {
			args.Limit = 20
		}
		q := url.Values{}
		q.Set("limit", fmt.Sprintf("%d", args.Limit))
		q.Set("tenant", args.Tenant)

		var resp listChatSessionsResponse
		if err := client.Get(ctx, "/v1/chat/sessions", q, &resp); err != nil {
			return CallToolResult{}, err
		}
		if len(resp.Data) == 0 {
			return CallToolResult{Content: TextContent("No chat sessions yet.")}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Found %d chat session(s):\n\n", len(resp.Data))
		for _, sess := range resp.Data {
			title := sess.Title
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "- %s · %s (%d turns)", shortID(sess.ID), title, sess.TurnCount)
			if sess.Tenant != "" {
				fmt.Fprintf(&b, " · tenant=%s", sess.Tenant)
			}
			if sess.UpdatedAt != "" {
				fmt.Fprintf(&b, " · updated %s", sess.UpdatedAt)
			}
			b.WriteByte('\n')
		}
		return CallToolResult{Content: TextContent(b.String())}, nil
	}
}

// ─── summarize_recent_traffic ────────────────────────────────────────

type summarizeArgs struct {
	Limit int `json:"limit"`
}

type traceListResponse struct {
	Data []struct {
		RequestID  string  `json:"request_id"`
		StartedAt  string  `json:"started_at"`
		FinishedAt string  `json:"finished_at,omitempty"`
		DurationMS int64   `json:"duration_ms,omitempty"`
		Provider   string  `json:"provider,omitempty"`
		Model      string  `json:"model,omitempty"`
		StatusCode string  `json:"status_code,omitempty"`
		StatusErr  bool    `json:"-"`
		Tokens     int64   `json:"total_tokens,omitempty"`
		CostUSD    float64 `json:"cost_usd,omitempty"`
	} `json:"data"`
}

func summarizeRecentTrafficHandler(client *HTTPClient) ToolHandler {
	return func(ctx context.Context, raw json.RawMessage) (CallToolResult, error) {
		var args summarizeArgs
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &args)
		}
		if args.Limit <= 0 {
			args.Limit = 100
		}
		q := url.Values{}
		q.Set("limit", fmt.Sprintf("%d", args.Limit))

		var resp traceListResponse
		if err := client.Get(ctx, "/v1/traces", q, &resp); err != nil {
			return CallToolResult{}, err
		}
		if len(resp.Data) == 0 {
			return CallToolResult{Content: TextContent("No recent traffic.")}, nil
		}

		// Aggregate by provider; track error count and latency.
		type bucket struct {
			count   int
			errors  int
			totalMS int64
			tokens  int64
			cost    float64
		}
		byProvider := map[string]*bucket{}
		var totalCount, totalErrors int
		var totalLatency int64
		for _, t := range resp.Data {
			provider := t.Provider
			if provider == "" {
				provider = "unknown"
			}
			b, ok := byProvider[provider]
			if !ok {
				b = &bucket{}
				byProvider[provider] = b
			}
			b.count++
			b.totalMS += t.DurationMS
			b.tokens += t.Tokens
			b.cost += t.CostUSD
			isError := t.StatusCode == "error" || strings.HasPrefix(t.StatusCode, "5") || strings.HasPrefix(t.StatusCode, "4")
			if isError {
				b.errors++
				totalErrors++
			}
			totalCount++
			totalLatency += t.DurationMS
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Recent traffic (last %d requests):\n", totalCount)
		fmt.Fprintf(&b, "  Total errors: %d (%.1f%%)\n", totalErrors, percent(totalErrors, totalCount))
		if totalCount > 0 {
			fmt.Fprintf(&b, "  Avg latency: %dms\n", totalLatency/int64(totalCount))
		}
		b.WriteString("\nBy provider:\n")
		for name, agg := range byProvider {
			avg := int64(0)
			if agg.count > 0 {
				avg = agg.totalMS / int64(agg.count)
			}
			fmt.Fprintf(&b, "  - %s: %d req, %d errors (%.1f%%), avg %dms",
				name, agg.count, agg.errors, percent(agg.errors, agg.count), avg)
			if agg.tokens > 0 {
				fmt.Fprintf(&b, ", %d tokens", agg.tokens)
			}
			if agg.cost > 0 {
				fmt.Fprintf(&b, ", $%.4f", agg.cost)
			}
			b.WriteByte('\n')
		}
		return CallToolResult{Content: TextContent(b.String())}, nil
	}
}

// ─── helpers ────────────────────────────────────────────────────────

// shortID truncates a UUID-ish identifier to its first 8 chars for
// readability. Full ID still appears in the wrapping detail tools.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func percent(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) * 100 / float64(whole)
}
