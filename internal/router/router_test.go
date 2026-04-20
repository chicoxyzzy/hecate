package router

import (
	"context"
	"testing"

	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestRuleRouterRoute(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{name: "openai", kind: providers.KindCloud, defaultModel: "gpt-4o-mini", allowAnyModel: true},
		&fakeProvider{name: "local", kind: providers.KindLocal, defaultModel: "llama3.1:8b", supportedModels: []string{"llama3.1:8b"}},
	)
	router := NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", registry)

	tests := []struct {
		name       string
		req        types.ChatRequest
		wantModel  string
		wantReason string
	}{
		{
			name: "explicit model wins",
			req: types.ChatRequest{
				Model: "gpt-4.1-mini",
			},
			wantModel:  "gpt-4.1-mini",
			wantReason: "explicit_model",
		},
		{
			name:       "default model is selected",
			req:        types.ChatRequest{},
			wantModel:  "gpt-4o-mini",
			wantReason: "default_model",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := router.Route(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if got.Model != tt.wantModel {
				t.Fatalf("Route() model = %q, want %q", got.Model, tt.wantModel)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Route() reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestRuleRouterRouteLocalFirst(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{name: "openai", kind: providers.KindCloud, defaultModel: "gpt-4o-mini", allowAnyModel: true},
		&fakeProvider{name: "local", kind: providers.KindLocal, defaultModel: "llama3.1:8b", supportedModels: []string{"llama3.1:8b"}},
	)
	router := NewRuleRouter("openai", "gpt-4o-mini", "local_first", "openai", registry)

	got, err := router.Route(context.Background(), types.ChatRequest{})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got.Provider != "local" {
		t.Fatalf("Route() provider = %q, want local", got.Provider)
	}
	if got.Model != "llama3.1:8b" {
		t.Fatalf("Route() model = %q, want llama3.1:8b", got.Model)
	}
}

type fakeProvider struct {
	name            string
	kind            providers.Kind
	defaultModel    string
	supportedModels []string
	allowAnyModel   bool
	capabilities    providers.Capabilities
	capabilitiesErr error
}

func (p *fakeProvider) Name() string         { return p.name }
func (p *fakeProvider) Kind() providers.Kind { return p.kind }
func (p *fakeProvider) DefaultModel() string { return p.defaultModel }
func (p *fakeProvider) Chat(_ context.Context, _ types.ChatRequest) (*types.ChatResponse, error) {
	return nil, nil
}
func (p *fakeProvider) Capabilities(_ context.Context) (providers.Capabilities, error) {
	if p.capabilitiesErr != nil {
		return providers.Capabilities{
			Name:         p.name,
			Kind:         p.kind,
			DefaultModel: p.defaultModel,
			Models:       append([]string(nil), p.supportedModels...),
		}, p.capabilitiesErr
	}
	if p.capabilities.Name != "" || len(p.capabilities.Models) > 0 || p.capabilities.DefaultModel != "" {
		return p.capabilities, nil
	}
	return providers.Capabilities{
		Name:         p.name,
		Kind:         p.kind,
		DefaultModel: p.defaultModel,
		Models:       append([]string(nil), p.supportedModels...),
	}, nil
}
func (p *fakeProvider) Supports(model string) bool {
	if p.allowAnyModel {
		return true
	}
	for _, candidate := range p.supportedModels {
		if candidate == model {
			return true
		}
	}
	return p.defaultModel != "" && p.defaultModel == model
}

func TestRuleRouterUsesDiscoveredCapabilities(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{
			name:         "local",
			kind:         providers.KindLocal,
			defaultModel: "configured-model",
			capabilities: providers.Capabilities{
				Name:         "local",
				Kind:         providers.KindLocal,
				DefaultModel: "discovered-model",
				Models:       []string{"discovered-model", "specialized-model"},
			},
		},
	)
	router := NewRuleRouter("local", "configured-model", "explicit_or_default", "", registry)

	got, err := router.Route(context.Background(), types.ChatRequest{})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got.Model != "discovered-model" {
		t.Fatalf("Route() model = %q, want discovered-model", got.Model)
	}

	got, err = router.Route(context.Background(), types.ChatRequest{Model: "specialized-model"})
	if err != nil {
		t.Fatalf("Route() explicit error = %v", err)
	}
	if got.Provider != "local" {
		t.Fatalf("Route() provider = %q, want local", got.Provider)
	}
}

func TestRuleRouterHonorsExplicitProvider(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{name: "openai", kind: providers.KindCloud, defaultModel: "gpt-4o-mini", allowAnyModel: true},
		&fakeProvider{name: "ollama", kind: providers.KindLocal, defaultModel: "llama3.1:8b", supportedModels: []string{"llama3.1:8b", "llama3.2:3b"}},
	)
	router := NewRuleRouter("openai", "gpt-4o-mini", "explicit_or_default", "", registry)

	got, err := router.Route(context.Background(), types.ChatRequest{
		Metadata: map[string]string{
			"provider": "ollama",
		},
	})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got.Provider != "ollama" {
		t.Fatalf("Route() provider = %q, want ollama", got.Provider)
	}
	if got.Model != "llama3.1:8b" {
		t.Fatalf("Route() model = %q, want llama3.1:8b", got.Model)
	}
	if got.Reason != "explicit_provider" {
		t.Fatalf("Route() reason = %q, want explicit_provider", got.Reason)
	}
}

func TestRuleRouterLocalFirstFallsBackWhenLocalIsUnhealthy(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{name: "openai", kind: providers.KindCloud, defaultModel: "gpt-4o-mini", allowAnyModel: true},
		&fakeProvider{
			name:            "ollama",
			kind:            providers.KindLocal,
			defaultModel:    "llama3.1:8b",
			supportedModels: []string{"llama3.1:8b"},
			capabilitiesErr: context.DeadlineExceeded,
		},
	)
	router := NewRuleRouter("openai", "gpt-4o-mini", "local_first", "openai", registry)

	got, err := router.Route(context.Background(), types.ChatRequest{})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got.Provider != "openai" {
		t.Fatalf("Route() provider = %q, want openai", got.Provider)
	}
	if got.Reason != "default_model_fallback_unhealthy_local" {
		t.Fatalf("Route() reason = %q, want default_model_fallback_unhealthy_local", got.Reason)
	}
}
