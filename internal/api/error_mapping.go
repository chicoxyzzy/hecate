package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/providers"
)

const (
	errCodeBudgetExceeded      = "budget_exceeded"
	errCodePriceMissing        = "price_missing"
	errCodeProviderAuthFailed  = "provider_auth_failed"
	errCodeProviderRateLimited = "provider_rate_limited"
	errCodeProviderUnavailable = "provider_unavailable"
	errCodeRouteImpossible     = "route_impossible"
	errCodeUnsupportedModel    = "unsupported_model"
)

type gatewayHTTPError struct {
	Status        int
	OpenAIType    string
	AnthropicType string
	Message       string
}

func classifyGatewayError(err error) gatewayHTTPError {
	message := gateway.UserFacingMessage(err)
	out := gatewayHTTPError{
		Status:        http.StatusInternalServerError,
		OpenAIType:    errCodeGatewayError,
		AnthropicType: "api_error",
		Message:       message,
	}
	if err == nil {
		out.Message = "gateway error"
		return out
	}

	if gateway.IsBudgetExceededError(err) {
		return gatewayHTTPError{
			Status:        http.StatusPaymentRequired,
			OpenAIType:    errCodeBudgetExceeded,
			AnthropicType: "payment_required",
			Message:       err.Error(),
		}
	}
	if gateway.IsRateLimitedError(err) {
		return gatewayHTTPError{
			Status:        http.StatusTooManyRequests,
			OpenAIType:    "rate_limit_exceeded",
			AnthropicType: "rate_limit_error",
			Message:       err.Error(),
		}
	}
	if billing.IsPriceNotFound(err) {
		out.Status = http.StatusFailedDependency
		out.OpenAIType = errCodePriceMissing
		out.Message = message
		return out
	}

	var upstreamErr *providers.UpstreamError
	if errors.As(err, &upstreamErr) {
		return classifyUpstreamError(upstreamErr)
	}

	if gateway.IsDeniedError(err) {
		out.Status = http.StatusForbidden
		out.OpenAIType = errCodeForbidden
		out.AnthropicType = "permission_error"
		return out
	}
	if gateway.IsClientError(err) {
		out.Status = http.StatusBadRequest
		out.OpenAIType = classifyClientErrorCode(message)
		out.AnthropicType = "invalid_request_error"
		return out
	}

	lower := strings.ToLower(message)
	switch {
	case isUnsupportedModelMessage(lower):
		out.Status = http.StatusBadRequest
		out.OpenAIType = errCodeUnsupportedModel
		out.AnthropicType = "invalid_request_error"
	case isRouteImpossibleMessage(lower):
		out.Status = http.StatusServiceUnavailable
		out.OpenAIType = errCodeRouteImpossible
		out.AnthropicType = "api_error"
	}
	return out
}

func classifyUpstreamError(err *providers.UpstreamError) gatewayHTTPError {
	status := mapUpstreamStatus(err.StatusCode)
	message := err.Message
	if strings.TrimSpace(message) == "" {
		message = "upstream provider error"
	}
	out := gatewayHTTPError{
		Status:        status,
		OpenAIType:    errCodeUpstreamError,
		AnthropicType: firstNonEmptyString(err.Type, "api_error"),
		Message:       message,
	}

	lower := strings.ToLower(err.Type + " " + message)
	switch {
	case err.StatusCode == http.StatusUnauthorized || err.StatusCode == http.StatusForbidden ||
		strings.Contains(lower, "incorrect api key") ||
		strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "authentication"):
		out.Status = http.StatusBadGateway
		out.OpenAIType = errCodeProviderAuthFailed
		out.AnthropicType = "authentication_error"
	case err.StatusCode == http.StatusTooManyRequests:
		out.Status = http.StatusTooManyRequests
		out.OpenAIType = errCodeProviderRateLimited
		out.AnthropicType = "rate_limit_error"
	case err.StatusCode == http.StatusRequestTimeout ||
		err.StatusCode == http.StatusBadGateway ||
		err.StatusCode == http.StatusServiceUnavailable ||
		err.StatusCode == http.StatusGatewayTimeout ||
		err.StatusCode >= 500:
		out.Status = http.StatusBadGateway
		out.OpenAIType = errCodeProviderUnavailable
		out.AnthropicType = "api_error"
	case isUnsupportedModelMessage(lower):
		out.Status = http.StatusBadRequest
		out.OpenAIType = errCodeUnsupportedModel
		out.AnthropicType = "invalid_request_error"
	}
	return out
}

func classifyClientErrorCode(message string) string {
	if isUnsupportedModelMessage(strings.ToLower(message)) {
		return errCodeUnsupportedModel
	}
	return errCodeInvalidRequest
}

func isUnsupportedModelMessage(message string) bool {
	return strings.Contains(message, "unsupported model") ||
		strings.Contains(message, "does not support explicit model") ||
		strings.Contains(message, "no provider supports explicit model") ||
		(strings.Contains(message, "model") &&
			(strings.Contains(message, "does not exist") ||
				strings.Contains(message, "not found") ||
				strings.Contains(message, "do not have access")))
}

func isRouteImpossibleMessage(message string) bool {
	return strings.Contains(message, "no model available for routing") ||
		strings.Contains(message, "no provider available") ||
		strings.Contains(message, "has no default model for routing") ||
		strings.Contains(message, "provider ") && strings.Contains(message, " not found")
}

func writeOpenAIGatewayError(w http.ResponseWriter, classified gatewayHTTPError) {
	WriteError(w, classified.Status, classified.OpenAIType, classified.Message)
}

func writeAnthropicGatewayError(w http.ResponseWriter, classified gatewayHTTPError) {
	WriteJSON(w, classified.Status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    classified.AnthropicType,
			"message": classified.Message,
		},
	})
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
