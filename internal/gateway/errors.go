package gateway

import (
	"errors"

	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/ratelimit"
)

var (
	errDenied = errors.New("request denied")
	errClient = errors.New("client error")
)

func IsDeniedError(err error) bool {
	return errors.Is(err, errDenied)
}

func IsClientError(err error) bool {
	return errors.Is(err, errClient)
}

// IsBudgetExceededError returns true when err is (or wraps) a
// governor.BudgetExceededError — callers should return HTTP 402.
func IsBudgetExceededError(err error) bool {
	var target *governor.BudgetExceededError
	return errors.As(err, &target)
}

// AsBudgetExceededError extracts the BudgetExceededError from err if present.
func AsBudgetExceededError(err error) (*governor.BudgetExceededError, bool) {
	var target *governor.BudgetExceededError
	ok := errors.As(err, &target)
	return target, ok
}

// IsRateLimitedError returns true when err is a ratelimit.ExceededError —
// callers should return HTTP 429.
func IsRateLimitedError(err error) bool {
	var target *ratelimit.ExceededError
	return errors.As(err, &target)
}

// AsRateLimitedError extracts the ratelimit.ExceededError from err if present.
func AsRateLimitedError(err error) (*ratelimit.ExceededError, bool) {
	var target *ratelimit.ExceededError
	ok := errors.As(err, &target)
	return target, ok
}
