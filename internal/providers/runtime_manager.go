package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/secrets"
)

type ControlPlaneRuntimeManager struct {
	logger      *slog.Logger
	baseConfigs []config.OpenAICompatibleProviderConfig
	store       controlplane.Store
	cipher      secrets.Cipher
	registry    *MutableRegistry
}

func NewControlPlaneRuntimeManager(logger *slog.Logger, baseConfigs []config.OpenAICompatibleProviderConfig, store controlplane.Store, cipher secrets.Cipher) *ControlPlaneRuntimeManager {
	items := buildProviders(baseConfigs, logger)
	return &ControlPlaneRuntimeManager{
		logger:      logger,
		baseConfigs: append([]config.OpenAICompatibleProviderConfig(nil), baseConfigs...),
		store:       store,
		cipher:      cipher,
		registry:    NewMutableRegistry(items...),
	}
}

func (m *ControlPlaneRuntimeManager) Registry() Registry {
	return m.registry
}

func (m *ControlPlaneRuntimeManager) SecretStorageEnabled() bool {
	return m.cipher != nil
}

func (m *ControlPlaneRuntimeManager) Reload(ctx context.Context) error {
	configs, err := m.resolvedConfigs(ctx)
	if err != nil {
		return err
	}
	m.registry.Replace(buildProviders(configs, m.logger)...)
	return nil
}

func (m *ControlPlaneRuntimeManager) Upsert(ctx context.Context, provider controlplane.Provider, apiKey string) (controlplane.Provider, error) {
	provider = hydrateControlPlaneProviderDefaults(provider)

	var encryptedSecret *controlplane.ProviderSecret
	if strings.TrimSpace(apiKey) != "" {
		if m.cipher == nil {
			return controlplane.Provider{}, fmt.Errorf("control plane secret storage is not configured")
		}
		encrypted, err := m.cipher.Encrypt(apiKey)
		if err != nil {
			return controlplane.Provider{}, fmt.Errorf("encrypt provider secret: %w", err)
		}
		encryptedSecret = &controlplane.ProviderSecret{
			ProviderID:      provider.ID,
			APIKeyEncrypted: encrypted,
			APIKeyPreview:   previewSecret(apiKey),
		}
	}

	if provider.Kind == "" {
		provider.Kind = string(KindCloud)
	}
	if provider.Protocol == "" {
		provider.Protocol = "openai"
	}
	if provider.Kind == string(KindCloud) && encryptedSecret == nil {
		state, err := m.snapshot(ctx)
		if err != nil {
			return controlplane.Provider{}, err
		}
		existing := findControlPlaneProvider(state.Providers, provider.ID, provider.Name)
		if existing == nil || !providerHasSecret(state, existing.ID) {
			return controlplane.Provider{}, fmt.Errorf("cloud providers require an api key")
		}
	}

	saved, err := m.store.UpsertProvider(ctx, provider, encryptedSecret)
	if err != nil {
		return controlplane.Provider{}, err
	}
	if err := m.Reload(ctx); err != nil {
		return controlplane.Provider{}, err
	}
	return saved, nil
}

func (m *ControlPlaneRuntimeManager) SetEnabled(ctx context.Context, id string, enabled bool) (controlplane.Provider, error) {
	saved, err := m.store.SetProviderEnabled(ctx, id, enabled)
	if err != nil {
		return controlplane.Provider{}, err
	}
	if err := m.Reload(ctx); err != nil {
		return controlplane.Provider{}, err
	}
	return saved, nil
}

func (m *ControlPlaneRuntimeManager) RotateSecret(ctx context.Context, id, apiKey string) (controlplane.Provider, error) {
	if m.cipher == nil {
		return controlplane.Provider{}, fmt.Errorf("control plane secret storage is not configured")
	}
	if strings.TrimSpace(apiKey) == "" {
		return controlplane.Provider{}, fmt.Errorf("provider api key is required")
	}
	encrypted, err := m.cipher.Encrypt(apiKey)
	if err != nil {
		return controlplane.Provider{}, fmt.Errorf("encrypt provider secret: %w", err)
	}
	saved, err := m.store.RotateProviderSecret(ctx, id, controlplane.ProviderSecret{
		ProviderID:      id,
		APIKeyEncrypted: encrypted,
		APIKeyPreview:   previewSecret(apiKey),
	})
	if err != nil {
		return controlplane.Provider{}, err
	}
	if err := m.Reload(ctx); err != nil {
		return controlplane.Provider{}, err
	}
	return saved, nil
}

func (m *ControlPlaneRuntimeManager) Delete(ctx context.Context, id string) error {
	if err := m.store.DeleteProvider(ctx, id); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *ControlPlaneRuntimeManager) resolvedConfigs(ctx context.Context) ([]config.OpenAICompatibleProviderConfig, error) {
	configs := append([]config.OpenAICompatibleProviderConfig(nil), m.baseConfigs...)
	if m.store == nil {
		return configs, nil
	}

	state, err := m.store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	byName := make(map[string]config.OpenAICompatibleProviderConfig, len(configs))
	order := make([]string, 0, len(configs)+len(state.Providers))
	for _, cfg := range configs {
		byName[cfg.Name] = cfg
		order = append(order, cfg.Name)
	}

	for _, item := range state.Providers {
		if !item.Enabled {
			continue
		}
		item = hydrateControlPlaneProviderDefaults(item)
		apiKey := ""
		if item.CredentialID != "" {
			if m.cipher == nil {
				m.logger.Warn("skipping control-plane provider without secret storage configured", slog.String("provider", item.Name))
				continue
			}
			secret := controlPlaneProviderSecretByProviderID(state.ProviderSecrets, item.ID)
			if secret == nil {
				m.logger.Warn("skipping control-plane provider with missing secret", slog.String("provider", item.Name))
				continue
			}
			decrypted, err := m.cipher.Decrypt(secret.APIKeyEncrypted)
			if err != nil {
				m.logger.Warn("skipping control-plane provider with undecryptable secret", slog.String("provider", item.Name), slog.Any("error", err))
				continue
			}
			apiKey = decrypted
		}
		cfg := config.OpenAICompatibleProviderConfig{
			Name:          item.Name,
			Kind:          item.Kind,
			Protocol:      item.Protocol,
			BaseURL:       item.BaseURL,
			APIKey:        apiKey,
			APIVersion:    item.APIVersion,
			DefaultModel:  item.DefaultModel,
			Models:        append([]string(nil), item.Models...),
			AllowAnyModel: item.AllowAnyModel,
			Timeout:       30 * time.Second,
		}
		if _, ok := byName[cfg.Name]; !ok {
			order = append(order, cfg.Name)
		}
		byName[cfg.Name] = cfg
	}

	out := make([]config.OpenAICompatibleProviderConfig, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	for _, name := range order {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cfg := byName[name]
		if cfg.Timeout == 0 {
			cfg.Timeout = 30 * time.Second
		}
		out = append(out, cfg)
	}
	return out, nil
}

func hydrateControlPlaneProviderDefaults(provider controlplane.Provider) controlplane.Provider {
	for _, candidate := range []string{provider.ID, provider.Name} {
		builtIn, ok := config.BuiltInProviderByID(candidate)
		if !ok {
			continue
		}
		minimalPreset := strings.TrimSpace(provider.Kind) == "" &&
			strings.TrimSpace(provider.Protocol) == "" &&
			strings.TrimSpace(provider.BaseURL) == "" &&
			strings.TrimSpace(provider.APIVersion) == "" &&
			strings.TrimSpace(provider.DefaultModel) == "" &&
			len(provider.Models) == 0 &&
			!provider.AllowAnyModel
		if strings.TrimSpace(provider.Name) == "" {
			provider.Name = builtIn.ID
		}
		if strings.TrimSpace(provider.Kind) == "" {
			provider.Kind = builtIn.Kind
		}
		if strings.TrimSpace(provider.Protocol) == "" {
			provider.Protocol = builtIn.Protocol
		}
		if strings.TrimSpace(provider.BaseURL) == "" {
			provider.BaseURL = builtIn.BaseURL
		}
		if strings.TrimSpace(provider.APIVersion) == "" {
			provider.APIVersion = builtIn.APIVersion
		}
		if strings.TrimSpace(provider.DefaultModel) == "" {
			provider.DefaultModel = builtIn.DefaultModel
		}
		if len(provider.Models) == 0 {
			provider.Models = append([]string(nil), builtIn.ExampleModels...)
		}
		if minimalPreset {
			provider.AllowAnyModel = builtIn.AllowAnyModel
		}
		return provider
	}
	return provider
}

func (m *ControlPlaneRuntimeManager) snapshot(ctx context.Context) (controlplane.State, error) {
	if m.store == nil {
		return controlplane.State{}, nil
	}
	return m.store.Snapshot(ctx)
}

func buildProviders(configs []config.OpenAICompatibleProviderConfig, logger *slog.Logger) []Provider {
	items := make([]Provider, 0, len(configs))
	for _, providerCfg := range configs {
		switch strings.ToLower(strings.TrimSpace(providerCfg.Protocol)) {
		case "anthropic":
			items = append(items, NewAnthropicProvider(providerCfg, logger))
		default:
			items = append(items, NewOpenAICompatibleProvider(providerCfg, logger))
		}
	}
	return items
}

func findControlPlaneProvider(items []controlplane.Provider, id, name string) *controlplane.Provider {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	for i := range items {
		if id != "" && items[i].ID == id {
			return &items[i]
		}
		if name != "" && items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func providerHasSecret(state controlplane.State, id string) bool {
	for _, secret := range state.ProviderSecrets {
		if secret.ProviderID == id && secret.APIKeyEncrypted != "" {
			return true
		}
	}
	return false
}

func controlPlaneProviderSecretByProviderID(items []controlplane.ProviderSecret, providerID string) *controlplane.ProviderSecret {
	for i := range items {
		if items[i].ProviderID == providerID {
			return &items[i]
		}
	}
	return nil
}

func previewSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 2 {
		return secret
	}
	if len(secret) <= 8 {
		return secret[:2] + "..." + secret[len(secret)-2:]
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}
