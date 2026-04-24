package config

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

type BuiltInProvider struct {
	ID           string
	Name         string
	Kind         string
	Protocol     string
	BaseURL      string
	APIKeyEnv    string
	APIVersion   string
	DefaultModel string
	DocsURL      string
	Description  string
	StubResponse string
}

var builtInProviders = []BuiltInProvider{
	{
		ID:           "openai",
		Name:         "OpenAI",
		Kind:         "cloud",
		Protocol:     "openai",
		BaseURL:      "https://api.openai.com",
		APIKeyEnv:    "PROVIDER_OPENAI_API_KEY",
		DefaultModel: "gpt-5.4-mini",
		DocsURL:      "https://developers.openai.com/api/docs/models",
		Description:  "Default cloud preset using the OpenAI-compatible Chat Completions API. Hecate discovers available models from /v1/models.",
		StubResponse: "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:           "anthropic",
		Name:         "Anthropic",
		Kind:         "cloud",
		Protocol:     "anthropic",
		BaseURL:      "https://api.anthropic.com",
		APIKeyEnv:    "PROVIDER_ANTHROPIC_API_KEY",
		APIVersion:   "2023-06-01",
		DefaultModel: "claude-sonnet-4-6",
		DocsURL:      "https://platform.claude.com/docs/en/about-claude/models/overview",
		Description:  "Native Anthropic Messages API preset. This uses Hecate's Anthropic protocol path and discovers available models from /v1/models.",
		StubResponse: "Stubbed Anthropic response.",
	},
	{
		ID:           "groq",
		Name:         "Groq",
		Kind:         "cloud",
		Protocol:     "openai",
		BaseURL:      "https://api.groq.com/openai/v1",
		APIKeyEnv:    "PROVIDER_GROQ_API_KEY",
		DefaultModel: "llama-3.3-70b-versatile",
		DocsURL:      "https://console.groq.com/docs/models",
		Description:  "OpenAI-compatible preset for Groq's low-latency inference API. Hecate discovers available models from /v1/models.",
		StubResponse: "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:           "gemini",
		Name:         "Gemini",
		Kind:         "cloud",
		Protocol:     "openai",
		BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
		APIKeyEnv:    "PROVIDER_GEMINI_API_KEY",
		DefaultModel: "gemini-2.5-flash",
		DocsURL:      "https://ai.google.dev/gemini-api/docs/openai",
		Description:  "OpenAI-compatible preset for Gemini through Google's compatibility layer. Hecate discovers available models from /v1/models.",
		StubResponse: "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:           "grok",
		Name:         "Grok (xAI)",
		Kind:         "cloud",
		Protocol:     "openai",
		BaseURL:      "https://api.x.ai/v1",
		APIKeyEnv:    "PROVIDER_GROK_API_KEY",
		DefaultModel: "grok-3-mini",
		DocsURL:      "https://docs.x.ai/docs/models",
		Description:  "OpenAI-compatible preset for xAI Grok models. Hecate discovers available models from /v1/models.",
		StubResponse: "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:           "ollama",
		Name:         "Ollama",
		Kind:         "local",
		Protocol:     "openai",
		BaseURL:      "http://127.0.0.1:11434/v1",
		DocsURL:      "https://github.com/ollama/ollama/blob/main/docs/openai.md",
		Description:  "Local preset for Ollama's OpenAI-compatible endpoint. Hecate discovers models from /v1/models and uses the first available model when no model is pinned.",
		StubResponse: "Stubbed local provider response.",
	},
	{
		ID:           "lmstudio",
		Name:         "LM Studio",
		Kind:         "local",
		Protocol:     "openai",
		BaseURL:      "http://127.0.0.1:1234/v1",
		DocsURL:      "https://lmstudio.ai/docs/app/api/endpoints/openai",
		Description:  "Local preset for LM Studio's OpenAI-compatible server. Hecate discovers models from /v1/models and uses the first available model when no model is pinned.",
		StubResponse: "Stubbed local provider response.",
	},
	{
		ID:           "localai",
		Name:         "LocalAI",
		Kind:         "local",
		Protocol:     "openai",
		BaseURL:      "http://127.0.0.1:8080/v1",
		DocsURL:      "https://localai.io/features/openai-functions/",
		Description:  "Local preset for LocalAI's OpenAI-compatible API surface. Hecate discovers models from /v1/models and uses the first available model when no model is pinned.",
		StubResponse: "Stubbed local provider response.",
	},
	{
		ID:           "llamacpp",
		Name:         "llama.cpp server",
		Kind:         "local",
		Protocol:     "openai",
		BaseURL:      "http://127.0.0.1:8080/v1",
		DocsURL:      "https://github.com/ggerganov/llama.cpp/tree/master/examples/server",
		Description:  "Local preset for llama.cpp-compatible OpenAI endpoints. Hecate discovers models from /v1/models and uses the first available model when no model is pinned.",
		StubResponse: "Stubbed local provider response.",
	},
}

func BuiltInProviders() []BuiltInProvider {
	out := make([]BuiltInProvider, len(builtInProviders))
	copy(out, builtInProviders)
	slices.SortFunc(out, func(a, b BuiltInProvider) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func BuiltInProviderByID(name string) (BuiltInProvider, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	normalized := builtInProviderLookupKey(name)
	for _, item := range builtInProviders {
		if item.ID == name || strings.ToLower(item.Name) == name || builtInProviderLookupKey(item.ID) == normalized || builtInProviderLookupKey(item.Name) == normalized {
			return item, true
		}
	}
	return BuiltInProvider{}, false
}

var builtInProviderLookupSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

func builtInProviderLookupKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return builtInProviderLookupSanitizer.ReplaceAllString(value, "")
}

func (p BuiltInProvider) RuntimeConfig(globalDefaultModel string) OpenAICompatibleProviderConfig {
	defaultModel := p.DefaultModel
	if p.ID == "openai" && strings.TrimSpace(globalDefaultModel) != "" {
		defaultModel = globalDefaultModel
	}
	return OpenAICompatibleProviderConfig{
		Name:         p.ID,
		Kind:         p.Kind,
		Protocol:     p.Protocol,
		BaseURL:      p.BaseURL,
		APIVersion:   p.APIVersion,
		Timeout:      30 * time.Second,
		StubMode:     false,
		StubResponse: p.StubResponse,
		DefaultModel: defaultModel,
	}
}
