package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Common errors
var (
	ErrEmbeddingFailed   = errors.New("embedding generation failed")
	ErrProviderNotReady  = errors.New("embedding provider not ready")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
	ErrInvalidInput      = errors.New("invalid input for embedding")
	ErrEmptyInput        = errors.New("empty input provided")
)

// Provider defines the interface for embedding generation
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	HealthCheck(ctx context.Context) error
}

// ProviderType represents the type of embedding provider
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderCohere    ProviderType = "cohere"
	ProviderLocal     ProviderType = "local"
)

// ProviderConfig contains common provider configuration
type ProviderConfig struct {
	Type         ProviderType
	APIKey       string
	BaseURL      string
	Model        string
	Dimension    int
	Timeout      time.Duration
	MaxRetries   int
	RetryDelay   time.Duration
	MaxBatchSize int
	RateLimitRPS float64
}

// DefaultProviderConfig returns default configuration
func DefaultProviderConfig() *ProviderConfig {
	return &ProviderConfig{
		Type:         ProviderOpenAI,
		BaseURL:      "https://api.openai.com/v1",
		Model:        "text-embedding-3-small",
		Dimension:    1536,
		Timeout:      30 * time.Second,
		MaxRetries:   3,
		RetryDelay:   time.Second,
		MaxBatchSize: 100,
		RateLimitRPS: 100,
	}
}

// OpenAIProvider implements Provider using OpenAI's embedding API
type OpenAIProvider struct {
	config     *ProviderConfig
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewOpenAIProvider creates a new OpenAI embedding provider
func NewOpenAIProvider(config *ProviderConfig) (*OpenAIProvider, error) {
	if config == nil {
		config = DefaultProviderConfig()
	}
	if config.APIKey == "" {
		return nil, errors.New("API key is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.Model == "" {
		config.Model = "text-embedding-3-small"
	}
	if config.Dimension == 0 {
		config.Dimension = 1536
	}

	return &OpenAIProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Embed generates an embedding for a single text
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, ErrEmptyInput
	}

	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, ErrEmbeddingFailed
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmptyInput
	}

	// Filter empty strings
	validTexts := make([]string, 0, len(texts))
	validIndices := make([]int, 0, len(texts))
	for i, text := range texts {
		if text != "" {
			validTexts = append(validTexts, text)
			validIndices = append(validIndices, i)
		}
	}

	if len(validTexts) == 0 {
		return nil, ErrEmptyInput
	}

	// Process in batches
	allEmbeddings := make([][]float32, len(texts))
	batchSize := p.config.MaxBatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	for i := 0; i < len(validTexts); i += batchSize {
		end := i + batchSize
		if end > len(validTexts) {
			end = len(validTexts)
		}
		batch := validTexts[i:end]
		batchIndices := validIndices[i:end]

		embeddings, err := p.embedBatchInternal(ctx, batch)
		if err != nil {
			return nil, err
		}

		for j, embedding := range embeddings {
			allEmbeddings[batchIndices[j]] = embedding
		}
	}

	return allEmbeddings, nil
}

func (p *OpenAIProvider) embedBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	// Prepare request
	reqBody := openAIEmbeddingRequest{
		Input: texts,
		Model: p.config.Model,
	}

	// Add dimensions parameter for models that support it
	if p.config.Model == "text-embedding-3-small" || p.config.Model == "text-embedding-3-large" {
		reqBody.Dimensions = p.config.Dimension
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make request with retries
	var lastErr error
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(p.config.RetryDelay * time.Duration(attempt)):
			}
		}

		embeddings, err := p.doRequest(ctx, jsonBody)
		if err == nil {
			return embeddings, nil
		}

		lastErr = err
		if errors.Is(err, ErrRateLimitExceeded) {
			continue
		}
		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, lastErr)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimitExceeded
	}

	if resp.StatusCode != http.StatusOK {
		var errResp openAIErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("API error: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var result openAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for _, item := range result.Data {
		embeddings[item.Index] = float64sToFloat32s(item.Embedding)
	}

	return embeddings, nil
}

// Dimension returns the embedding dimension
func (p *OpenAIProvider) Dimension() int {
	return p.config.Dimension
}

// HealthCheck verifies the provider is working
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Embed(ctx, "health check")
	return err
}

// OpenAI API types
type openAIEmbeddingRequest struct {
	Input      []string `json:"input"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbeddingResponse struct {
	Data  []openAIEmbeddingData `json:"data"`
	Model string                `json:"model"`
	Usage openAIUsage           `json:"usage"`
}

type openAIEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type openAIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// LocalProvider implements Provider using a local embedding server
type LocalProvider struct {
	config     *ProviderConfig
	httpClient *http.Client
}

// NewLocalProvider creates a new local embedding provider
func NewLocalProvider(config *ProviderConfig) (*LocalProvider, error) {
	if config == nil {
		config = &ProviderConfig{
			BaseURL:      "http://localhost:8000",
			Dimension:    384,
			Timeout:      30 * time.Second,
			MaxRetries:   3,
			RetryDelay:   time.Second,
			MaxBatchSize: 100,
		}
	}

	return &LocalProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}, nil
}

// Embed generates an embedding for a single text
func (p *LocalProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, ErrEmptyInput
	}

	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, ErrEmbeddingFailed
	}

	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts
func (p *LocalProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmptyInput
	}

	reqBody := localEmbeddingRequest{
		Texts: texts,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.config.BaseURL+"/embed", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var result localEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Embeddings, nil
}

// Dimension returns the embedding dimension
func (p *LocalProvider) Dimension() int {
	return p.config.Dimension
}

// HealthCheck verifies the provider is working
func (p *LocalProvider) HealthCheck(ctx context.Context) error {
	_, err := p.Embed(ctx, "health check")
	return err
}

type localEmbeddingRequest struct {
	Texts []string `json:"texts"`
}

type localEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// MockProvider implements Provider for testing
type MockProvider struct {
	dimension int
	mu        sync.Mutex
	calls     []string
}

// NewMockProvider creates a new mock provider
func NewMockProvider(dimension int) *MockProvider {
	if dimension <= 0 {
		dimension = 384
	}
	return &MockProvider{
		dimension: dimension,
		calls:     make([]string, 0),
	}
}

// Embed generates a deterministic mock embedding
func (p *MockProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	p.mu.Lock()
	p.calls = append(p.calls, text)
	p.mu.Unlock()

	return p.generateMockEmbedding(text), nil
}

// EmbedBatch generates mock embeddings for multiple texts
func (p *MockProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		embeddings[i] = embedding
	}
	return embeddings, nil
}

// Dimension returns the embedding dimension
func (p *MockProvider) Dimension() int {
	return p.dimension
}

// HealthCheck always returns nil for mock provider
func (p *MockProvider) HealthCheck(ctx context.Context) error {
	return nil
}

// GetCalls returns the recorded calls (for testing)
func (p *MockProvider) GetCalls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.calls...)
}

// ClearCalls clears recorded calls (for testing)
func (p *MockProvider) ClearCalls() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = make([]string, 0)
}

func (p *MockProvider) generateMockEmbedding(text string) []float32 {
	embedding := make([]float32, p.dimension)

	// Generate deterministic embedding based on text hash
	hash := simpleHash(text)
	for i := 0; i < p.dimension; i++ {
		// Generate pseudo-random value between -1 and 1
		hash = (hash*1103515245 + 12345) & 0x7fffffff
		embedding[i] = float32(hash%2001-1000) / 1000.0
	}

	// Normalize to unit length
	normalize(embedding)

	return embedding
}

// Helper functions

func float64sToFloat32s(f64s []float64) []float32 {
	f32s := make([]float32, len(f64s))
	for i, v := range f64s {
		f32s[i] = float32(v)
	}
	return f32s
}

func isRetryableError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func simpleHash(s string) uint32 {
	var h uint32 = 5381
	for _, c := range s {
		h = ((h << 5) + h) + uint32(c)
	}
	return h
}

func normalize(v []float32) {
	var sum float32
	for _, val := range v {
		sum += val * val
	}
	if sum > 0 {
		norm := float32(1.0 / float64(sum))
		for i := range v {
			v[i] *= norm
		}
	}
}

// NewProvider creates a provider based on configuration
func NewProvider(config *ProviderConfig) (Provider, error) {
	if config == nil {
		config = DefaultProviderConfig()
	}

	switch config.Type {
	case ProviderOpenAI:
		return NewOpenAIProvider(config)
	case ProviderLocal:
		return NewLocalProvider(config)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", config.Type)
	}
}
