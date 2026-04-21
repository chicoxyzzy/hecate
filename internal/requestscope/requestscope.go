package requestscope

import (
	"slices"
	"strings"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/pkg/types"
)

func Build(principal auth.Principal, tenant, provider string) types.RequestScope {
	scope := types.RequestScope{
		Tenant:       strings.TrimSpace(tenant),
		User:         strings.TrimSpace(tenant),
		ProviderHint: strings.TrimSpace(provider),
		Principal: types.PrincipalContext{
			Role:             strings.TrimSpace(principal.Role),
			Tenant:           strings.TrimSpace(principal.Tenant),
			AllowedProviders: normalizeList(principal.AllowedProviders),
			AllowedModels:    normalizeList(principal.AllowedModels),
		},
	}
	scope.AllowedProviders = append([]string(nil), scope.Principal.AllowedProviders...)
	scope.AllowedModels = append([]string(nil), scope.Principal.AllowedModels...)
	return Normalize(scope)
}

func FromChatRequest(req types.ChatRequest) types.RequestScope {
	return Normalize(req.Scope)
}

func Normalize(scope types.RequestScope) types.RequestScope {
	scope.Tenant = strings.TrimSpace(scope.Tenant)
	scope.User = strings.TrimSpace(scope.User)
	scope.ProviderHint = strings.TrimSpace(scope.ProviderHint)
	scope.AllowedProviders = normalizeList(scope.AllowedProviders)
	scope.AllowedModels = normalizeList(scope.AllowedModels)

	scope.Principal.Role = strings.TrimSpace(scope.Principal.Role)
	scope.Principal.Tenant = strings.TrimSpace(scope.Principal.Tenant)
	scope.Principal.AllowedProviders = normalizeList(scope.Principal.AllowedProviders)
	scope.Principal.AllowedModels = normalizeList(scope.Principal.AllowedModels)

	if scope.User == "" {
		scope.User = scope.Tenant
	}
	if scope.Tenant == "" {
		scope.Tenant = scope.Principal.Tenant
	}
	if scope.Principal.Tenant == "" {
		scope.Principal.Tenant = scope.Tenant
	}
	if len(scope.AllowedProviders) == 0 {
		scope.AllowedProviders = append([]string(nil), scope.Principal.AllowedProviders...)
	}
	if len(scope.AllowedModels) == 0 {
		scope.AllowedModels = append([]string(nil), scope.Principal.AllowedModels...)
	}
	if len(scope.Principal.AllowedProviders) == 0 {
		scope.Principal.AllowedProviders = append([]string(nil), scope.AllowedProviders...)
	}
	if len(scope.Principal.AllowedModels) == 0 {
		scope.Principal.AllowedModels = append([]string(nil), scope.AllowedModels...)
	}
	return scope
}

func EffectiveTenant(scope types.RequestScope, fallback string) string {
	scope = Normalize(scope)
	switch {
	case scope.Tenant != "":
		return scope.Tenant
	case scope.User != "":
		return scope.User
	case scope.Principal.Tenant != "":
		return scope.Principal.Tenant
	case strings.TrimSpace(fallback) != "":
		return strings.TrimSpace(fallback)
	default:
		return "anonymous"
	}
}

func normalizeList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}
