package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// StdioTransport speaks newline-delimited JSON-RPC over a child
// process's stdin/stdout. This is the canonical MCP-stdio framing:
// each line is one complete JSON envelope, trailing `\n` is the
// frame delimiter, no length prefix.
//
// Stderr is captured to an internal ring buffer (capStderr) so a
// crashing server's last words can be surfaced in error messages
// without flooding the operator's logs. We deliberately don't
// inherit the parent process stderr — an MCP server that's chatty
// on stderr would otherwise drown the gateway's own log stream.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	stderr *ringBuffer

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}

	// writeMu serializes Sends. The bufio.Reader on the read side is
	// only touched by a single goroutine per the Client contract, so
	// no read mutex is needed here.
	writeMu sync.Mutex
}

const stderrCapBytes = 8 * 1024

// NewStdioTransport spawns the given command and returns a transport
// wired to its stdio. The command must NOT be already started — this
// constructor calls Start() itself so it can hook up the pipes
// before the process produces output.
//
// The caller is responsible for choosing a command that speaks
// MCP-stdio (e.g. `npx @modelcontextprotocol/server-filesystem
// /some/dir`). Mismatched commands typically fail at the initialize
// handshake with a JSON parse error or an EOF.
//
// Cleanup contract: Close() terminates the process. The caller
// should always Close, even on error paths, to avoid orphaned
// children.
func NewStdioTransport(cmd *exec.Cmd) (*StdioTransport, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr := newRingBuffer(stderrCapBytes)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start mcp server: %w", err)
	}
	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 64*1024),
		stderr: stderr,
		closed: make(chan struct{}),
	}, nil
}

// Send writes one JSON-RPC envelope as a single line. The frame
// must NOT contain raw newlines — JSON-encoded RawMessage already
// satisfies this since json.Marshal escapes embedded newlines. We
// append the trailing `\n` ourselves so callers don't have to.
func (t *StdioTransport) Send(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-t.closed:
		return ErrTransportClosed
	default:
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	// Length-prefix isn't needed — MCP-stdio framing is just \n.
	// Some servers expect the writer to flush after each line; the
	// underlying os.Pipe is unbuffered so the syscall write IS the
	// flush.
	if _, err := t.stdin.Write(frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	if !bytes.HasSuffix(frame, []byte("\n")) {
		if _, err := t.stdin.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write framing newline: %w", err)
		}
	}
	return nil
}

// Recv reads one JSON-RPC envelope (one line). Blocks until either
// the line arrives, ctx is cancelled, or the peer closes.
//
// Cancellation note: bufio.Reader doesn't support context, so we
// run the read in a goroutine and select on ctx.Done(). When ctx
// fires, we close stdin (via Close) to unblock the read on most
// platforms — but the read goroutine may linger briefly. That's
// acceptable; the Client only Recvs from a single goroutine and
// shuts the transport down on its way out.
func (t *StdioTransport) Recv(ctx context.Context) ([]byte, error) {
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// ReadBytes returns the bytes including the delimiter,
		// which we strip — the caller wants a clean envelope.
		line, err := t.stdout.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		ch <- result{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closed:
		return nil, ErrTransportClosed
	case r := <-ch:
		switch {
		case r.err == nil:
			return r.line, nil
		case errors.Is(r.err, io.EOF):
			// Peer hung up. If we have a partial line buffered,
			// surface it — sometimes the server writes one last
			// frame without a trailing newline before exit.
			if len(r.line) > 0 {
				return r.line, nil
			}
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("read frame: %w", r.err)
		}
	}
}

// Close terminates the child process. Sequence:
//
//  1. Close stdin (signals EOF; well-behaved servers exit cleanly)
//  2. Wait for the process to exit
//  3. Close pipes; mark the transport closed
//
// Close is idempotent and safe to call concurrently with Recv.
func (t *StdioTransport) Close() error {
	t.closeOnce.Do(func() {
		// Closing stdin signals the child that no more requests are
		// coming. Most MCP servers exit when stdin closes; some need
		// a kill, so we follow up with cmd.Wait and a kill on
		// timeout in the caller (Client does this via ShutdownTimeout).
		_ = t.stdin.Close()
		// Wait blocks until the process exits. The bufio reader's
		// ReadBytes on stdout returns io.EOF once the child closes
		// its end, which unblocks any pending Recv.
		t.closeErr = t.cmd.Wait()
		close(t.closed)
	})
	return t.closeErr
}

// Stderr returns whatever the child has written to stderr so far.
// Useful for diagnostics — when a server crashes mid-handshake the
// JSON-RPC error is opaque ("EOF") and stderr usually has the real
// reason (Python traceback, missing dependency, bad config). We
// cap at 8 KiB so a chatty server can't grow the buffer
// unboundedly.
func (t *StdioTransport) Stderr() string {
	return t.stderr.String()
}

// ringBuffer is a fixed-cap io.Writer for stderr capture. New
// writes overwrite the oldest bytes when full. Not the most
// efficient possible (a real ring would be byte-addressable) but
// the cap is small so the simple slice rewrite is fine.
type ringBuffer struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{cap: cap, buf: make([]byte, 0, cap)}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		// Drop the oldest bytes — keep the most recent stderr
		// (the failure context is usually at the tail).
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
