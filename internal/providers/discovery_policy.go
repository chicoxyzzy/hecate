package providers

import (
	"errors"
	"net"
	"strings"
	"time"
)

const (
	capabilitiesSuccessTTL      = time.Minute
	capabilitiesUnconfiguredTTL = 10 * time.Minute
	capabilitiesFailureTTL      = time.Minute
	capabilitiesLocalFailureTTL = 30 * time.Second
)

func discoveryUnconfigured(kind Kind, apiKey string) bool {
	return kind == KindCloud && strings.TrimSpace(apiKey) == ""
}

func discoveryFailureTTL(kind Kind, err error) time.Duration {
	if kind == KindLocal && isLocalConnectionFailure(err) {
		return capabilitiesLocalFailureTTL
	}
	return capabilitiesFailureTTL
}

func isLocalConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}
