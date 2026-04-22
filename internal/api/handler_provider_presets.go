package api

import (
	"fmt"
	"net/http"
	"strings"
)

type providerPreset struct {
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
	EnvSnippet    string
}

func (h *Handler) HandleProviderPresets(w http.ResponseWriter, r *http.Request) {
	items := providerPresets()
	data := make([]ProviderPresetResponseItem, 0, len(items))
	for _, item := range items {
		data = append(data, ProviderPresetResponseItem{
			ID:            item.ID,
			Name:          item.Name,
			Kind:          item.Kind,
			Protocol:      item.Protocol,
			BaseURL:       item.BaseURL,
			APIKeyEnv:     item.APIKeyEnv,
			APIVersion:    item.APIVersion,
			DefaultModel:  item.DefaultModel,
			ExampleModels: append([]string(nil), item.ExampleModels...),
			DocsURL:       item.DocsURL,
			Description:   item.Description,
			EnvSnippet:    item.EnvSnippet,
		})
	}

	WriteJSON(w, http.StatusOK, ProviderPresetResponse{
		Object: "provider_presets",
		Data:   data,
	})
}

func providerPresets() []providerPreset {
	return []providerPreset{
		newProviderPreset(
			"openai",
			"OpenAI",
			"cloud",
			"openai",
			"https://api.openai.com",
			"PROVIDER_OPENAI_API_KEY",
			"",
			"gpt-4o-mini",
			[]string{"gpt-4o-mini", "gpt-4.1-mini"},
			"https://platform.openai.com/docs/api-reference/chat",
			"Default cloud preset using the OpenAI-compatible Chat Completions API.",
		),
		newProviderPreset(
			"anthropic",
			"Anthropic",
			"cloud",
			"anthropic",
			"https://api.anthropic.com",
			"PROVIDER_ANTHROPIC_API_KEY",
			"2023-06-01",
			"claude-sonnet-4-20250514",
			[]string{"claude-sonnet-4-20250514", "claude-haiku-3-5-20241022"},
			"https://docs.anthropic.com/en/api/messages",
			"Native Anthropic Messages API preset. This uses Hecate's Anthropic protocol path instead of OpenAI-compat mode.",
		),
		newProviderPreset(
			"groq",
			"Groq",
			"cloud",
			"openai",
			"https://api.groq.com/openai/v1",
			"PROVIDER_GROQ_API_KEY",
			"",
			"llama-3.3-70b-versatile",
			[]string{"llama-3.3-70b-versatile", "openai/gpt-oss-20b"},
			"https://console.groq.com/docs/openai",
			"OpenAI-compatible preset for Groq's low-latency inference API.",
		),
		newProviderPreset(
			"gemini",
			"Gemini",
			"cloud",
			"openai",
			"https://generativelanguage.googleapis.com/v1beta/openai",
			"PROVIDER_GEMINI_API_KEY",
			"",
			"gemini-2.5-flash",
			[]string{"gemini-2.5-flash", "gemini-2.5-pro"},
			"https://ai.google.dev/gemini-api/docs/openai",
			"OpenAI-compatible preset for Gemini through Google's compatibility layer.",
		),
		newProviderPreset(
			"ollama",
			"Ollama",
			"local",
			"openai",
			"http://127.0.0.1:11434/v1",
			"",
			"",
			"llama3.1:8b",
			[]string{"llama3.1:8b", "qwen2.5:7b"},
			"https://github.com/ollama/ollama/blob/main/docs/openai.md",
			"Local preset for Ollama's OpenAI-compatible endpoint.",
		),
		newProviderPreset(
			"lmstudio",
			"LM Studio",
			"local",
			"openai",
			"http://127.0.0.1:1234/v1",
			"",
			"",
			"local-model",
			[]string{"local-model"},
			"https://lmstudio.ai/docs/app/api/endpoints/openai",
			"Local preset for LM Studio's OpenAI-compatible server.",
		),
		newProviderPreset(
			"localai",
			"LocalAI",
			"local",
			"openai",
			"http://127.0.0.1:8080/v1",
			"",
			"",
			"local-model",
			[]string{"local-model"},
			"https://localai.io/features/openai-functions/",
			"Local preset for LocalAI's OpenAI-compatible API surface.",
		),
		newProviderPreset(
			"llamacpp",
			"llama.cpp server",
			"local",
			"openai",
			"http://127.0.0.1:8080/v1",
			"",
			"",
			"local-model",
			[]string{"local-model"},
			"https://github.com/ggerganov/llama.cpp/tree/master/examples/server",
			"Local preset for llama.cpp-compatible OpenAI endpoints.",
		),
	}
}

func newProviderPreset(id, name, kind, protocol, baseURL, apiKeyEnv, apiVersion, defaultModel string, exampleModels []string, docsURL, description string) providerPreset {
	var envLines []string
	prefix := "PROVIDER_" + strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(id, "-", "_"), ".", "_")) + "_"
	envLines = append(envLines, fmt.Sprintf("GATEWAY_PROVIDERS=%s", id))
	if apiKeyEnv != "" {
		envLines = append(envLines, fmt.Sprintf("%s=your_api_key_here", apiKeyEnv))
	}
	if apiVersion != "" {
		envLines = append(envLines, fmt.Sprintf("%s=%s", prefix+"API_VERSION", apiVersion))
	}
	if baseURL != "" {
		envLines = append(envLines, fmt.Sprintf("%s=%s", prefix+"BASE_URL", baseURL))
	}

	return providerPreset{
		ID:            id,
		Name:          name,
		Kind:          kind,
		Protocol:      protocol,
		BaseURL:       baseURL,
		APIKeyEnv:     apiKeyEnv,
		APIVersion:    apiVersion,
		DefaultModel:  defaultModel,
		ExampleModels: append([]string(nil), exampleModels...),
		DocsURL:       docsURL,
		Description:   description,
		EnvSnippet:    strings.Join(envLines, "\n"),
	}
}
