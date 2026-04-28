package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mcpJSONResponse is a minimal JSON-RPC 2.0 response used in HTTP tests.
type mcpJSONResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func jsonRPCOK(id any, result any) []byte {
	raw, _ := json.Marshal(result)
	resp := mcpJSONResponse{JSONRPC: "2.0", ID: id, Result: raw}
	out, _ := json.Marshal(resp)
	return out
}

// sseEvent formats a single SSE data event.
func sseEvent(data string) string {
	return "data: " + data + "\n\n"
}

// TestHTTPTransport_NewRejectsInvalidURL verifies that a non-URL string
// is caught at construction time so callers get a clear error instead of
// a runtime panic on the first Send.
func TestHTTPTransport_NewRejectsInvalidURL(t *testing.T) {
	t.Parallel()
	_, err := NewHTTPTransport("not-a-url", nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
	if !strings.Contains(err.Error(), "invalid endpoint URL") {
		t.Errorf("err = %v, want 'invalid endpoint URL'", err)
	}
}

// TestHTTPTransport_JSONResponseRoundTrip sends one JSON-RPC request and
// reads the JSON response back through Send/Recv.
func TestHTTPTransport_JSONResponseRoundTrip(t *testing.T) {
	t.Parallel()
	payload := jsonRPCOK(1, map[string]string{"status": "ok"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	frame := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if err := tr.Send(ctx, frame); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := tr.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("Recv = %s, want %s", got, payload)
	}
}

// TestHTTPTransport_SSEResponseDeliveredAsFrames verifies that a
// text/event-stream response is parsed and each event is delivered
// as a separate frame through Recv.
func TestHTTPTransport_SSEResponseDeliveredAsFrames(t *testing.T) {
	t.Parallel()
	msg1 := `{"jsonrpc":"2.0","id":1,"result":{"a":1}}`
	msg2 := `{"jsonrpc":"2.0","id":2,"result":{"b":2}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseEvent(msg1))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, sseEvent(msg2))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	for i, want := range []string{msg1, msg2} {
		got, err := tr.Recv(ctx)
		if err != nil {
			t.Fatalf("Recv[%d]: %v", i, err)
		}
		if string(got) != want {
			t.Errorf("Recv[%d] = %s, want %s", i, got, want)
		}
	}
}

// TestHTTPTransport_NonSuccessStatusIsError verifies that a 4xx or 5xx
// response is surfaced as a non-nil error from Send.
func TestHTTPTransport_NonSuccessStatusIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`))
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("err = %v, want 'HTTP 403'", err)
	}
}

// TestHTTPTransport_SessionIDCapturedAndReplayed verifies that the first
// Mcp-Session-Id response header value is attached to subsequent requests.
func TestHTTPTransport_SessionIDCapturedAndReplayed(t *testing.T) {
	t.Parallel()
	const wantSID = "session-xyz-42"
	receivedSIDs := make(chan string, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSIDs <- r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Mcp-Session-Id", wantSID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsonRPCOK(1, "pong"))
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	frame := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)

	// First request — no session ID yet.
	if err := tr.Send(ctx, frame); err != nil {
		t.Fatalf("Send 1: %v", err)
	}
	if _, err := tr.Recv(ctx); err != nil {
		t.Fatalf("Recv 1: %v", err)
	}
	sid1 := <-receivedSIDs
	if sid1 != "" {
		t.Errorf("first request sent Mcp-Session-Id = %q, want empty", sid1)
	}

	// Second request — must carry the session ID from the first response.
	if err := tr.Send(ctx, frame); err != nil {
		t.Fatalf("Send 2: %v", err)
	}
	if _, err := tr.Recv(ctx); err != nil {
		t.Fatalf("Recv 2: %v", err)
	}
	sid2 := <-receivedSIDs
	if sid2 != wantSID {
		t.Errorf("second request Mcp-Session-Id = %q, want %q", sid2, wantSID)
	}
}

// TestHTTPTransport_StaticHeadersForwarded verifies that the headers map
// passed to NewHTTPTransport is sent on every outbound request.
func TestHTTPTransport_StaticHeadersForwarded(t *testing.T) {
	t.Parallel()
	gotAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsonRPCOK(1, "ok"))
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, map[string]string{
		"Authorization": "Bearer test-token",
	}, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := tr.Recv(ctx); err != nil {
		t.Fatalf("Recv: %v", err)
	}

	auth := <-gotAuth
	if auth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want 'Bearer test-token'", auth)
	}
}

// TestHTTPTransport_CloseUnblocksRecv verifies that calling Close while
// Recv is blocked returns ErrTransportClosed promptly.
func TestHTTPTransport_CloseUnblocksRecv(t *testing.T) {
	t.Parallel()

	// Server that sends the first request header immediately and then
	// streams nothing — Recv will block indefinitely without Close.
	hangCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		// Block until the test signals done (server-side cleanup).
		<-hangCh
	}))
	t.Cleanup(func() {
		close(hangCh)
		srv.Close()
	})

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := tr.Recv(ctx)
		done <- err
	}()

	// Give the goroutine a moment to block on Recv, then close.
	time.Sleep(20 * time.Millisecond)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrTransportClosed) {
			t.Errorf("Recv after Close = %v, want ErrTransportClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recv did not unblock after Close within 2s")
	}
}

// TestHTTPTransport_EmptyBodyDiscarded verifies that a 202 Accepted
// with an empty body (common for notification acknowledgements) doesn't
// push anything to recvCh — Recv should not unblock.
func TestHTTPTransport_EmptyBodyDiscarded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","method":"notify"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Recv should time out — empty body produces no frame.
	_, err = tr.Recv(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Recv after empty body = %v, want DeadlineExceeded", err)
	}
}

// TestHTTPTransport_MultiLineSSEEvent verifies that a multi-line SSE
// event (multiple consecutive "data:" lines) is concatenated and
// delivered as a single frame with embedded newlines.
func TestHTTPTransport_MultiLineSSEEvent(t *testing.T) {
	t.Parallel()
	line1 := `{"partial":true`
	line2 := `,"more":true}`
	// The test deliberately sends a multi-data-line event; the join
	// produces: line1 + "\n" + line2.
	want := line1 + "\n" + line2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: "+line1+"\n")
		_, _ = io.WriteString(w, "data: "+line2+"\n")
		_, _ = io.WriteString(w, "\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := tr.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if string(got) != want {
		t.Errorf("Recv = %q, want %q", got, want)
	}
}

// TestHTTPTransport_SSEIgnoresCommentAndEventLines verifies that SSE
// comment lines (": ..."), "event:" lines, "id:" lines, and "retry:"
// lines are silently ignored — only "data:" carries the payload.
func TestHTTPTransport_SSEIgnoresCommentAndEventLines(t *testing.T) {
	t.Parallel()
	payload := `{"jsonrpc":"2.0","id":1,"result":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		body := ": this is a keepalive comment\n" +
			"event: message\n" +
			"id: 42\n" +
			"retry: 1000\n" +
			"data: " + payload + "\n" +
			"\n"
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	tr, err := NewHTTPTransport(srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"x"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := tr.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if string(got) != payload {
		t.Errorf("Recv = %q, want %q", got, payload)
	}
}
