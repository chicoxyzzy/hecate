package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
)

type Principal struct {
	Name             string
	Role             string
	Tenant           string
	Source           string
	KeyID            string
	AllowedProviders []string
	AllowedModels    []string
}

func (p Principal) IsAdmin() bool {
	return p.Role == "admin"
}

type Authenticator struct {
	adminToken          string
	singleUserAdminMode bool
	store               controlplane.Store
	enabled             bool
}

func NewAuthenticator(cfg config.ServerConfig, store controlplane.Store) *Authenticator {
	return &Authenticator{
		adminToken:          cfg.AuthToken,
		singleUserAdminMode: cfg.SingleUserAdminMode,
		store:               store,
		enabled:             cfg.AuthToken != "" || store != nil,
	}
}

func (a *Authenticator) Enabled() bool {
	return a != nil && a.enabled
}

func (a *Authenticator) Authenticate(r *http.Request) (Principal, bool) {
	if a == nil {
		return Principal{Role: "anonymous"}, true
	}
	if a.singleUserAdminMode {
		return singleUserAdminPrincipal(), true
	}
	if !a.enabled {
		return Principal{Role: "anonymous"}, true
	}

	token := requestToken(r)
	if token == "" {
		return Principal{}, false
	}

	if a.adminToken != "" && token == a.adminToken {
		return a.normalizePrincipal(Principal{
			Name:   "admin",
			Role:   "admin",
			Source: "admin_token",
		}), true
	}

	if a.store != nil {
		state, err := a.store.Snapshot(context.Background())
		if err == nil {
			for _, key := range state.APIKeys {
				if !key.Enabled || key.Key != token {
					continue
				}
				if key.Tenant != "" && !tenantEnabled(state.Tenants, key.Tenant) {
					return Principal{}, false
				}
				principal := Principal{
					Name:             key.Name,
					Role:             key.Role,
					Tenant:           key.Tenant,
					Source:           "control_plane_api_key",
					KeyID:            key.ID,
					AllowedProviders: mergedAllowlist(key.AllowedProviders, tenantAllowProviders(state.Tenants, key.Tenant)),
					AllowedModels:    mergedAllowlist(key.AllowedModels, tenantAllowModels(state.Tenants, key.Tenant)),
				}
				return a.normalizePrincipal(principal), true
			}
		}
	}
	return Principal{}, false
}

type Introspection struct {
	Authenticated bool
	InvalidToken  bool
	Principal     Principal
}

func (a *Authenticator) Introspect(r *http.Request) Introspection {
	if a == nil {
		return Introspection{
			Authenticated: false,
			Principal: Principal{
				Role:   "anonymous",
				Source: "auth_disabled",
			},
		}
	}
	if a.singleUserAdminMode {
		return Introspection{
			Authenticated: true,
			Principal:     singleUserAdminPrincipal(),
		}
	}
	if !a.enabled {
		return Introspection{
			Authenticated: false,
			Principal: Principal{
				Role:   "anonymous",
				Source: "auth_disabled",
			},
		}
	}

	token := requestToken(r)
	if token == "" {
		return Introspection{
			Authenticated: false,
			Principal: Principal{
				Role:   "anonymous",
				Source: "no_token",
			},
		}
	}

	principal, ok := a.Authenticate(r)
	if !ok {
		return Introspection{
			Authenticated: false,
			InvalidToken:  true,
			Principal: Principal{
				Role:   "invalid",
				Source: "invalid_token",
			},
		}
	}

	return Introspection{
		Authenticated: true,
		Principal:     principal,
	}
}

// requestToken extracts the client auth token.
//
// Precedence:
// 1. Authorization: Bearer <token>
// 2. x-api-key: <token>
//
// This keeps behavior deterministic when both are present.
func requestToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

func singleUserAdminPrincipal() Principal {
	return Principal{
		Name:   "single-user",
		Role:   "admin",
		Source: "single_user_admin_mode",
	}
}

func (a *Authenticator) normalizePrincipal(principal Principal) Principal {
	if !a.singleUserAdminMode {
		return principal
	}
	if principal.IsAdmin() {
		return principal
	}
	if principal.Source == "control_plane_api_key" {
		principal.Role = "admin"
		principal.Source = "single_user_admin_mode"
	}
	return principal
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func mergedAllowlist(primary, fallback []string) []string {
	if len(primary) > 0 && len(fallback) > 0 {
		out := make([]string, 0, len(primary))
		for _, item := range primary {
			for _, candidate := range fallback {
				if item == candidate {
					out = append(out, item)
					break
				}
			}
		}
		return out
	}
	if len(primary) > 0 {
		return append([]string(nil), primary...)
	}
	if len(fallback) > 0 {
		return append([]string(nil), fallback...)
	}
	return nil
}

func tenantAllowProviders(tenants []controlplane.Tenant, id string) []string {
	for _, tenant := range tenants {
		if tenant.ID == id && tenant.Enabled {
			return tenant.AllowedProviders
		}
	}
	return nil
}

func tenantAllowModels(tenants []controlplane.Tenant, id string) []string {
	for _, tenant := range tenants {
		if tenant.ID == id && tenant.Enabled {
			return tenant.AllowedModels
		}
	}
	return nil
}

func tenantEnabled(tenants []controlplane.Tenant, id string) bool {
	for _, tenant := range tenants {
		if tenant.ID == id {
			return tenant.Enabled
		}
	}
	return false
}
