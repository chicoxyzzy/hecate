package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// httpRecvBufSize is the capacity of the inbound message channel.
	// Buffering absorbs bursts of SSE events from a single request
	// without blocking the goroutine that's reading the response body.
	httpRecvBufSize = 64

	// httpMaxBodyBytes caps a single JSON response body. SSE streams
	// are not capped here — each individual event is bounded by the
	// server; we only limit non-streaming JSON responses.
	httpMaxBodyBytes = 8 << 20 // 8 MiB
)

// HTTPTransport implements Transport for MCP's Streamable HTTP protocol
// (https://spec.modelcontextprotocol.io/specification/basic/transports/).
//
// Each Send POSTs one JSON-RPC message to the endpoint URL. The server
// responds with either application/json (a single message) or
// text/event-stream (SSE, zero or more messages). All incoming frames
// are pushed to a shared receive channel; Client's read loop
// demultiplexes them by JSON-RPC id.
//
// Session management: if the server returns Mcp-Session-Id on any
// response, it is captured and attached to all subsequent requests.
// The first value wins; it is never updated mid-session.
//
// Closing: Close cancels the transport's internal context, which
// interrupts any goroutines blocked on reading response bodies (because
// in-flight request bodies are associated with that context via the
// underlying HTTP connections). All background goroutines are waited on
// before Close returns.
type HTTPTransport struct {
	endpoint string
	headers  map[string]string // static outbound headers (e.g. Authorization)
	httpCli  *http.Client

	// Lifetime context: cancelled by Close to unblock in-flight body reads.
	ctx    context.Context
	cancel context.CancelFunc

	recvCh    chan []byte   // inbound JSON-RPC frames
	closed    chan struct{} // closed once by Close
	closeOnce sync.Once

	// errCh carries the first fatal background error to Recv. Buffered
	// so the sending goroutine never blocks.
	errCh chan error

	// wg tracks goroutines reading response bodies.
	wg sync.WaitGroup

	// sessionID is populated from the Mcp-Session-Id response header.
	sessionMu sync.Mutex
	sid       string
}

// NewHTTPTransport creates a transport for the MCP Streamable HTTP
// protocol. endpoint must be a valid absolute URL (e.g.
// "https://api.example.com/mcp"). headers are attached to every
// outbound request — the typical use is {"Authorization": "Bearer …"}.
// httpCli may be nil; a default client with a 5-minute timeout is used
// in that case (generous enough for slow tool calls, finite enough to
// prevent goroutine leaks on hung servers).
func NewHTTPTransport(endpoint string, headers map[string]string, httpCli *http.Client) (*HTTPTransport, error) {
	if _, err := url.ParseRequestURI(endpoint); err != nil {
		return nil, fmt.Errorf("mcp http: invalid endpoint URL %q: %w", endpoint, err)
	}
	if httpCli == nil {
		httpCli = &http.Client{Timeout: 5 * time.Minute}
	}
	hdr := make(map[string]string, len(headers))
	for k, v := range headers {
		hdr[k] = v
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &HTTPTransport{
		endpoint: endpoint,
		headers:  hdr,
		httpCli:  httpCli,
		ctx:      ctx,
		cancel:   cancel,
		recvCh:   make(chan []byte, httpRecvBufSize),
		errCh:    make(chan error, 1),
		closed:   make(chan struct{}),
	}, nil
}

// Send POSTs frame to the endpoint and launches a goroutine to read
// the response body (JSON or SSE) into the receive channel. Send
// returns once the request is dispatched and the response headers are
// received; it does NOT wait for the full body.
//
// The HTTP request is made with a context that is cancelled by either
// ctx or Close, whichever fires first. This ensures that when Close is
// called, any in-flight body reads are interrupted promptly rather than
// waiting for a server timeout.
func (t *HTTPTransport) Send(ctx context.Context, frame []byte) error {
	select {
	case <-t.closed:
		return ErrTransportClosed
	default:
	}

	// Merge the call context with the transport's lifetime context so
	// that Close() interrupts the HTTP call even if ctx has no deadline.
	reqCtx, reqCancel := context.WithCancel(t.ctx)
	// Cancel reqCtx when the caller's context fires.
	go func() {
		select {
		case <-ctx.Done():
			reqCancel()
		case <-reqCtx.Done():
		}
	}()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.endpoint, bytes.NewReader(frame))
	if err != nil {
		reqCancel()
		return fmt.Errorf("mcp http: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if sid := t.getSessionID(); sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := t.httpCli.Do(req)
	if err != nil {
		reqCancel()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			select {
			case <-t.closed:
				return ErrTransportClosed
			default:
			}
		}
		return fmt.Errorf("mcp http: post %s: %w", t.endpoint, err)
	}

	// Capture session ID from the first response that carries one.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.setSessionID(sid)
	}

	// Non-2xx status: read a short diagnostic excerpt and fail.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		reqCancel()
		return fmt.Errorf("mcp http: server returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(excerpt)))
	}

	ct := resp.Header.Get("Content-Type")
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer resp.Body.Close()
		defer reqCancel() // release the context watcher goroutine

		var readErr error
		switch {
		case strings.Contains(ct, "text/event-stream"):
			readErr = t.drainSSE(resp.Body)
		case strings.Contains(ct, "application/json"):
			readErr = t.drainJSON(resp.Body)
		default:
			// 202 Accepted or empty body — no JSON-RPC content to parse.
			// Drain any small body so the connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		}
		if readErr != nil && !isClosedErr(readErr) {
			select {
			case t.errCh <- fmt.Errorf("mcp http: read response: %w", readErr):
			default:
			}
		}
	}()
	return nil
}

// Recv blocks until a frame arrives, the transport is closed, or ctx
// is cancelled. Returns ErrTransportClosed after Close.
func (t *HTTPTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case frame, ok := <-t.recvCh:
		if !ok {
			return nil, ErrTransportClosed
		}
		return frame, nil
	case err := <-t.errCh:
		return nil, err
	case <-t.closed:
		return nil, ErrTransportClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close cancels the transport's internal context (which interrupts any
// goroutines currently reading HTTP response bodies), waits for them
// to exit, then signals the receive channel. Idempotent.
func (t *HTTPTransport) Close() error {
	t.closeOnce.Do(func() {
		t.cancel() // interrupt in-flight body reads
		close(t.closed)
	})
	t.wg.Wait()
	return nil
}

// drainJSON reads a single JSON body, trims whitespace, and pushes it
// to recvCh if non-empty. Empty bodies are silently discarded (they
// occur on 202 Accepted notification acknowledgements).
func (t *HTTPTransport) drainJSON(body io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(body, httpMaxBodyBytes))
	if err != nil {
		return err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	select {
	case t.recvCh <- raw:
	case <-t.closed:
	}
	return nil
}

// drainSSE reads a Server-Sent Events stream and pushes each data
// event's payload to recvCh. It returns when the stream ends
// (io.EOF) or encounters a read error. Blank lines delimit events;
// lines beginning with "data:" carry the JSON-RPC payload.
// Multi-line events (multiple consecutive "data:" lines) are
// concatenated with newlines before being pushed.
func (t *HTTPTransport) drainSSE(body io.Reader) error {
	scanner := bufio.NewScanner(body)
	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			// Trim the mandatory single leading space (RFC 8895 §3.1).
			data := line[5:]
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			dataLines = append(dataLines, data)
		case line == "":
			// Blank line terminates the event.
			if len(dataLines) > 0 {
				payload := []byte(strings.Join(dataLines, "\n"))
				dataLines = dataLines[:0]
				// Skip SSE ping / keepalive events (common pattern).
				if bytes.TrimSpace(payload) != nil && len(bytes.TrimSpace(payload)) > 0 {
					select {
					case t.recvCh <- payload:
					case <-t.closed:
						return nil
					}
				}
			}
			// Ignore "event:", "id:", "retry:", and ": comment" lines.
		}
	}
	// Dispatch any unterminated final event (stream closed without a
	// trailing blank line — non-standard but tolerated).
	if len(dataLines) > 0 {
		payload := []byte(strings.Join(dataLines, "\n"))
		if p := bytes.TrimSpace(payload); len(p) > 0 {
			select {
			case t.recvCh <- payload:
			case <-t.closed:
			}
		}
	}
	return scanner.Err()
}

func (t *HTTPTransport) getSessionID() string {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	return t.sid
}

func (t *HTTPTransport) setSessionID(id string) {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	if t.sid == "" {
		t.sid = id
	}
}

// isClosedErr reports whether err represents a normal transport-close
// condition that should not be propagated as a fatal error. These
// arise when the HTTP connection is cut by Close() cancelling the
// request context.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
