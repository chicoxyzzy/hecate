package sandbox

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPolicyErrorErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *PolicyError
		want string
	}{
		{"nil receiver returns generic", nil, "sandbox policy denied"},
		{"empty reason returns generic", &PolicyError{Reason: ""}, "sandbox policy denied"},
		{"whitespace reason returns generic", &PolicyError{Reason: "   "}, "sandbox policy denied"},
		{"reason is appended", &PolicyError{Reason: "no exec"}, "sandbox policy denied: no exec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePathRequiresTarget(t *testing.T) {
	cases := []string{"", "   "}
	for _, target := range cases {
		t.Run("empty="+target, func(t *testing.T) {
			if _, err := ResolvePath(".", target, Policy{}); err == nil {
				t.Errorf("ResolvePath(target=%q) → err = nil, want error", target)
			}
		})
	}
}

func TestResolvePathEnforcesAllowedRoot(t *testing.T) {
	root := t.TempDir()

	// Inside the root: must succeed.
	resolved, err := ResolvePath(root, "subdir/file.txt", Policy{AllowedRoot: root})
	if err != nil {
		t.Fatalf("ResolvePath inside root: %v", err)
	}
	if !strings.HasPrefix(resolved, root) {
		t.Errorf("resolved %q does not start with root %q", resolved, root)
	}

	// Escapes the root: must return PolicyError.
	if _, err := ResolvePath(root, "../escape.txt", Policy{AllowedRoot: root}); err == nil {
		t.Error("expected PolicyError on path that escapes allowed root")
	} else if !IsPolicyDenied(err) {
		t.Errorf("expected PolicyError, got %T: %v", err, err)
	}
}

func TestCommandMutatesStateDetectsRedirects(t *testing.T) {
	// Both " >" and ">>" patterns should be classified as mutating.
	mutating := []string{
		"echo hi > /tmp/out",
		"echo hi >> /tmp/out",
		"rm -rf /tmp/foo",
		"git add .",
	}
	for _, cmd := range mutating {
		if !commandMutatesState(cmd) {
			t.Errorf("commandMutatesState(%q) = false, want true", cmd)
		}
	}

	nonMutating := []string{
		"ls -la",
		"echo hi",
		"git status",
		"cat /etc/hosts",
	}
	for _, cmd := range nonMutating {
		if commandMutatesState(cmd) {
			t.Errorf("commandMutatesState(%q) = true, want false", cmd)
		}
	}
}

func TestClassifyWorkerError(t *testing.T) {
	if got := classifyWorkerError(&PolicyError{Reason: "x"}); got != "policy" {
		t.Errorf("classifyWorkerError(policy) = %q, want policy", got)
	}
	if got := classifyWorkerError(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("classifyWorkerError(deadline) = %q, want timeout", got)
	}
	if got := classifyWorkerError(errors.New("other")); got != "generic" {
		t.Errorf("classifyWorkerError(other) = %q, want generic", got)
	}
}

func TestDecodeWorkerError(t *testing.T) {
	t.Run("policy reconstructs PolicyError with stripped prefix", func(t *testing.T) {
		err := decodeWorkerError("sandbox policy denied: no write", "policy")
		if !IsPolicyDenied(err) {
			t.Fatalf("expected PolicyError, got %T", err)
		}
		var pe *PolicyError
		_ = errors.As(err, &pe)
		if pe.Reason != "no write" {
			t.Errorf("Reason = %q, want %q", pe.Reason, "no write")
		}
	})

	t.Run("timeout maps to context.DeadlineExceeded", func(t *testing.T) {
		err := decodeWorkerError("timed out", "timeout")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", err)
		}
	})

	t.Run("generic preserves the message", func(t *testing.T) {
		err := decodeWorkerError("kaboom", "generic")
		if err == nil || err.Error() != "kaboom" {
			t.Errorf("expected error message %q, got %v", "kaboom", err)
		}
	})
}

func TestWorkerCommandErrorRespectsContextCancellation(t *testing.T) {
	t.Run("context cancelled wins over stderr", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		stderr := bytes.NewBufferString("noisy error from worker")
		got := workerCommandError(ctx, stderr, errors.New("exit 1"))
		if !errors.Is(got, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", got)
		}
	})

	t.Run("context deadline wins over stderr", func(t *testing.T) {
		// Set a deadline that's already past so ctx.Err() returns
		// DeadlineExceeded by the time workerCommandError checks it.
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		stderr := bytes.NewBufferString("ignored")
		got := workerCommandError(ctx, stderr, errors.New("exit 1"))
		if !errors.Is(got, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded, got %v", got)
		}
	})

	t.Run("stderr message wraps the failure", func(t *testing.T) {
		ctx := context.Background()
		stderr := bytes.NewBufferString("worker rejected payload")
		got := workerCommandError(ctx, stderr, errors.New("exit 1"))
		if !strings.Contains(got.Error(), "worker rejected payload") {
			t.Errorf("error %q should contain stderr text", got.Error())
		}
	})

	t.Run("falls back to err.Error when stderr empty", func(t *testing.T) {
		ctx := context.Background()
		got := workerCommandError(ctx, bytes.NewBuffer(nil), errors.New("exit 1"))
		if !strings.Contains(got.Error(), "exit 1") {
			t.Errorf("error %q should contain raw err message when stderr is empty", got.Error())
		}
	})
}

func TestDecodeWorkerResponseMissingResponseField(t *testing.T) {
	// Valid event JSON but with no nested response object — must error.
	buf := bytes.NewBufferString(`{"type":"end"}`)
	if _, err := decodeWorkerResponse(buf); err == nil {
		t.Error("expected error when response field is missing, got nil")
	}
}

func TestDecodeWorkerResponseMalformedJSON(t *testing.T) {
	buf := bytes.NewBufferString("{not valid json")
	if _, err := decodeWorkerResponse(buf); err == nil {
		t.Error("expected error on malformed JSON, got nil")
	}
}

func TestDecodeWorkerResponseSucceedsOnValidEvent(t *testing.T) {
	buf := bytes.NewBufferString(`{"type":"end","response":{"error":"","error_kind":""}}`)
	resp, err := decodeWorkerResponse(buf)
	if err != nil {
		t.Fatalf("decodeWorkerResponse: %v", err)
	}
	if resp.Error != "" || resp.ErrorKind != "" {
		t.Errorf("unexpected response: %+v", resp)
	}
}
