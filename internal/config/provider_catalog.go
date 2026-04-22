package config

import (
	"regexp"
	"strings"
	"time"
)

type BuiltInProvider struct {
	ID            string
	Name          string
	Kind          string
	Protocol      string
	BaseURL       string
	APIKeyEnv     string
	APIVersion    string
	DefaultModel  string
	ExampleModels []string
	DocsURL       string
	Description   string
	AllowAnyModel bool
	StubResponse  string
}

var builtInProviders = []BuiltInProvider{
	{
		ID:            "openai",
		Name:          "OpenAI",
		Kind:          "cloud",
		Protocol:      "openai",
		BaseURL:       "https://api.openai.com",
		APIKeyEnv:     "PROVIDER_OPENAI_API_KEY",
		DefaultModel:  "gpt-4o-mini",
		ExampleModels: []string{"gpt-4o-mini", "gpt-4.1-mini"},
		DocsURL:       "https://platform.openai.com/docs/api-reference/chat",
		Description:   "Default cloud preset using the OpenAI-compatible Chat Completions API.",
		AllowAnyModel: true,
		StubResponse:  "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:            "anthropic",
		Name:          "Anthropic",
		Kind:          "cloud",
		Protocol:      "anthropic",
		BaseURL:       "https://api.anthropic.com",
		APIKeyEnv:     "PROVIDER_ANTHROPIC_API_KEY",
		APIVersion:    "2023-06-01",
		DefaultModel:  "claude-sonnet-4-20250514",
		ExampleModels: []string{"claude-sonnet-4-20250514", "claude-haiku-3-5-20241022"},
		DocsURL:       "https://docs.anthropic.com/en/api/messages",
		Description:   "Native Anthropic Messages API preset. This uses Hecate's Anthropic protocol path instead of OpenAI-compat mode.",
		AllowAnyModel: true,
		StubResponse:  "Stubbed Anthropic response.",
	},
	{
		ID:            "groq",
		Name:          "Groq",
		Kind:          "cloud",
		Protocol:      "openai",
		BaseURL:       "https://api.groq.com/openai/v1",
		APIKeyEnv:     "PROVIDER_GROQ_API_KEY",
		DefaultModel:  "llama-3.3-70b-versatile",
		ExampleModels: []string{"llama-3.3-70b-versatile", "openai/gpt-oss-20b"},
		DocsURL:       "https://console.groq.com/docs/openai",
		Description:   "OpenAI-compatible preset for Groq's low-latency inference API.",
		AllowAnyModel: true,
		StubResponse:  "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:            "gemini",
		Name:          "Gemini",
		Kind:          "cloud",
		Protocol:      "openai",
		BaseURL:       "https://generativelanguage.googleapis.com/v1beta/openai",
		APIKeyEnv:     "PROVIDER_GEMINI_API_KEY",
		DefaultModel:  "gemini-2.5-flash",
		ExampleModels: []string{"gemini-2.5-flash", "gemini-2.5-pro"},
		DocsURL:       "https://ai.google.dev/gemini-api/docs/openai",
		Description:   "OpenAI-compatible preset for Gemini through Google's compatibility layer.",
		AllowAnyModel: true,
		StubResponse:  "Stubbed response from the AI Agent Runtime MVP.",
	},
	{
		ID:            "ollama",
		Name:          "Ollama",
		Kind:          "local",
		Protocol:      "openai",
		BaseURL:       "http://127.0.0.1:11434/v1",
		DefaultModel:  "llama3.1:8b",
		ExampleModels: []string{"llama3.1:8b", "qwen2.5:7b"},
		DocsURL:       "https://github.com/ollama/ollama/blob/main/docs/openai.md",
		Description:   "Local preset for Ollama's OpenAI-compatible endpoint.",
		AllowAnyModel: false,
		StubResponse:  "Stubbed local provider response.",
	},
	{
		ID:            "lmstudio",
		Name:          "LM Studio",
		Kind:          "local",
		Protocol:      "openai",
		BaseURL:       "http://127.0.0.1:1234/v1",
		DefaultModel:  "local-model",
		ExampleModels: []string{"local-model"},
		DocsURL:       "https://lmstudio.ai/docs/app/api/endpoints/openai",
		Description:   "Local preset for LM Studio's OpenAI-compatible server.",
		AllowAnyModel: false,
		StubResponse:  "Stubbed local provider response.",
	},
	{
		ID:            "localai",
		Name:          "LocalAI",
		Kind:          "local",
		Protocol:      "openai",
		BaseURL:       "http://127.0.0.1:8080/v1",
		DefaultModel:  "local-model",
		ExampleModels: []string{"local-model"},
		DocsURL:       "https://localai.io/features/openai-functions/",
		Description:   "Local preset for LocalAI's OpenAI-compatible API surface.",
		AllowAnyModel: false,
		StubResponse:  "Stubbed local provider response.",
	},
	{
		ID:            "llamacpp",
		Name:          "llama.cpp server",
		Kind:          "local",
		Protocol:      "openai",
		BaseURL:       "http://127.0.0.1:8080/v1",
		DefaultModel:  "local-model",
		ExampleModels: []string{"local-model"},
		DocsURL:       "https://github.com/ggerganov/llama.cpp/tree/master/examples/server",
		Description:   "Local preset for llama.cpp-compatible OpenAI endpoints.",
		AllowAnyModel: false,
		StubResponse:  "Stubbed local provider response.",
	},
}

func BuiltInProviders() []BuiltInProvider {
	out := make([]BuiltInProvider, len(builtInProviders))
	copy(out, builtInProviders)
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
		Name:          p.ID,
		Kind:          p.Kind,
		Protocol:      p.Protocol,
		BaseURL:       p.BaseURL,
		APIVersion:    p.APIVersion,
		Timeout:       30 * time.Second,
		StubMode:      false,
		StubResponse:  p.StubResponse,
		DefaultModel:  defaultModel,
		AllowAnyModel: p.AllowAnyModel,
	}
}
