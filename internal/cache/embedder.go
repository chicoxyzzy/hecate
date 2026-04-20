package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Embedder interface {
	Name() string
	Embed(ctx context.Context, text string) ([]float64, error)
}

type LocalSimpleEmbedder struct {
	MaxTextChars int
	Dimensions   int
}

func (e LocalSimpleEmbedder) Name() string {
	return "local_simple"
}

func (e LocalSimpleEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	if e.MaxTextChars > 0 && len(text) > e.MaxTextChars {
		text = text[:e.MaxTextChars]
	}
	tokens := tokenizeSemanticText(text)
	if len(tokens) == 0 {
		return nil, nil
	}

	dims := e.Dimensions
	if dims <= 0 {
		dims = 256
	}

	vector := make([]float64, dims)
	for _, token := range tokens {
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		idx := hasher.Sum64() % uint64(dims)
		vector[idx]++
	}
	return normalizeVector(vector), nil
}

type OpenAICompatibleEmbedder struct {
	name       string
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type OpenAICompatibleEmbedderConfig struct {
	Name       string
	BaseURL    string
	APIKey     string
	Model      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type openAIEmbeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbeddingsResponse struct {
	Data []openAIEmbeddingItem `json:"data"`
}

type openAIEmbeddingItem struct {
	Embedding []float64 `json:"embedding"`
}

type openAIErrorEnvelope struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}

type UpstreamError struct {
	StatusCode int
	Message    string
	Type       string
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.Type == "" {
		return fmt.Sprintf("upstream error (%d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("upstream error (%d/%s): %s", e.StatusCode, e.Type, e.Message)
}

func NewOpenAICompatibleEmbedder(cfg OpenAICompatibleEmbedderConfig) *OpenAICompatibleEmbedder {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "openai_compatible"
	}
	return &OpenAICompatibleEmbedder{
		name:       name,
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		model:      cfg.Model,
		httpClient: buildEmbedderHTTPClient(cfg.HTTPClient, timeout),
	}
}

func buildEmbedderHTTPClient(client *http.Client, timeout time.Duration) *http.Client {
	if client == nil {
		return &http.Client{Timeout: timeout}
	}
	cloned := *client
	if cloned.Timeout == 0 {
		cloned.Timeout = timeout
	}
	return &cloned
}

func (e *OpenAICompatibleEmbedder) Name() string {
	if e == nil {
		return ""
	}
	return e.name
}

func (e *OpenAICompatibleEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if e == nil {
		return nil, fmt.Errorf("embedder is nil")
	}
	if strings.TrimSpace(e.baseURL) == "" {
		return nil, fmt.Errorf("embedder base URL is required")
	}
	if strings.TrimSpace(e.model) == "" {
		return nil, fmt.Errorf("embedder model is required")
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	payload, err := json.Marshal(openAIEmbeddingsRequest{
		Model: e.model,
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embeddings request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildEmbeddingsURL(e.baseURL), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build embeddings request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send embeddings request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, decodeEmbedderUpstreamError(resp)
	}

	var wireResp openAIEmbeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&wireResp); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	if len(wireResp.Data) == 0 || len(wireResp.Data[0].Embedding) == 0 {
		return nil, nil
	}
	return normalizeVector(append([]float64(nil), wireResp.Data[0].Embedding...)), nil
}

func buildEmbeddingsURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/embeddings"
	}
	return trimmed + "/v1/embeddings"
}

func decodeEmbedderUpstreamError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read embeddings error body: %w", err)
	}

	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Message:    envelope.Error.Message,
			Type:       envelope.Error.Type,
		}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return &UpstreamError{
		StatusCode: resp.StatusCode,
		Message:    message,
	}
}

var semanticTokenizer = regexp.MustCompile(`[a-z0-9]+`)

func tokenizeSemanticText(text string) []string {
	raw := semanticTokenizer.FindAllString(strings.ToLower(text), -1)
	out := raw[:0]
	for _, token := range raw {
		if len(token) < 3 {
			continue
		}
		out = append(out, token)
	}
	return out
}

func normalizeVector(vector []float64) []float64 {
	var sum float64
	for _, value := range vector {
		sum += value * value
	}
	if sum == 0 {
		return vector
	}
	norm := math.Sqrt(sum)
	for i := range vector {
		vector[i] /= norm
	}
	return vector
}

func cosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var sum float64
	for i := range left {
		sum += left[i] * right[i]
	}
	return sum
}
