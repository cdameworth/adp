package context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Common errors
var (
	ErrInvalidRequest   = errors.New("invalid context request")
	ErrBudgetExceeded   = errors.New("token budget exceeded")
	ErrEmbeddingFailed  = errors.New("embedding generation failed")
	ErrRetrievalFailed  = errors.New("context retrieval failed")
	ErrServiceNotFound  = errors.New("service not found")
	ErrCacheUnavailable = errors.New("cache unavailable")
)

// ContextRequest represents a request for context assembly
type ContextRequest struct {
	SessionID   string       `json:"session_id"`
	ServiceID   string       `json:"service_id"`
	Task        string       `json:"task"`
	TokenBudget *TokenBudget `json:"token_budget,omitempty"`
	Filters     *Filters     `json:"filters,omitempty"`
}

// TokenBudget defines token limits per layer
type TokenBudget struct {
	Essential    int `json:"essential"`
	TaskRelevant int `json:"task_relevant"`
	Supporting   int `json:"supporting"`
	Reserved     int `json:"reserved"`
}

// DefaultTokenBudget returns the default token budget configuration
func DefaultTokenBudget() *TokenBudget {
	return &TokenBudget{
		Essential:    4000,
		TaskRelevant: 12000,
		Supporting:   8000,
		Reserved:     8000,
	}
}

// Total returns the total token budget
func (b *TokenBudget) Total() int {
	return b.Essential + b.TaskRelevant + b.Supporting + b.Reserved
}

// Filters defines optional filters for context retrieval
type Filters struct {
	FilePatterns    []string          `json:"file_patterns,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	MinRelevance    float64           `json:"min_relevance,omitempty"`
	ExcludePatterns []string          `json:"exclude_patterns,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// ContextResponse represents the assembled context
type ContextResponse struct {
	SessionID     string         `json:"session_id"`
	ServiceID     string         `json:"service_id"`
	Layers        []ContextLayer `json:"layers"`
	TotalTokens   int            `json:"total_tokens"`
	BudgetUsage   BudgetUsage    `json:"budget_usage"`
	Metadata      ResponseMeta   `json:"metadata"`
	CacheHit      bool           `json:"cache_hit"`
	RetrievalTime time.Duration  `json:"retrieval_time"`
}

// BudgetUsage tracks token usage per layer
type BudgetUsage struct {
	Essential    TokenUsage `json:"essential"`
	TaskRelevant TokenUsage `json:"task_relevant"`
	Supporting   TokenUsage `json:"supporting"`
}

// TokenUsage tracks used vs available tokens
type TokenUsage struct {
	Used      int `json:"used"`
	Available int `json:"available"`
}

// ResponseMeta contains metadata about the context assembly
type ResponseMeta struct {
	AssembledAt    time.Time `json:"assembled_at"`
	ContextVersion string    `json:"context_version"`
	SourceCount    int       `json:"source_count"`
	CacheKey       string    `json:"cache_key,omitempty"`
}

// ContextItem represents a single piece of context content
type ContextItem struct {
	ID        string                 `json:"id"`
	Content   string                 `json:"content"`
	Source    string                 `json:"source"`
	Tokens    int                    `json:"tokens"`
	Relevance float64                `json:"relevance"`
	LayerType LayerType              `json:"layer_type"`
	Tags      []string               `json:"tags"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// VectorStore interface for vector database operations
type VectorStore interface {
	Search(ctx context.Context, collection string, vector []float32, limit int) ([]SearchResult, error)
	SearchWithFilter(ctx context.Context, collection string, vector []float32, limit int, filter map[string]interface{}) ([]SearchResult, error)
	Upsert(ctx context.Context, collection string, items []VectorItem) error
	Delete(ctx context.Context, collection string, ids []string) error
	CreateCollection(ctx context.Context, collection string, dimension int) error
	CollectionExists(ctx context.Context, collection string) (bool, error)
}

// SearchResult represents a vector search result
type SearchResult struct {
	ID      string                 `json:"id"`
	Score   float64                `json:"score"`
	Payload map[string]interface{} `json:"payload"`
	Vector  []float32              `json:"vector,omitempty"`
}

// VectorItem represents an item to be stored in the vector database
type VectorItem struct {
	ID      string                 `json:"id"`
	Vector  []float32              `json:"vector"`
	Payload map[string]interface{} `json:"payload"`
}

// EmbeddingProvider interface for generating embeddings
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// Cache interface for context caching
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// ServiceRegistry provides service metadata
type ServiceRegistry interface {
	GetService(ctx context.Context, serviceID string) (*ServiceInfo, error)
	GetServiceContext(ctx context.Context, serviceID string) (*ServiceContextConfig, error)
}

// ServiceInfo contains service metadata
type ServiceInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Owner        string            `json:"owner"`
	Team         string            `json:"team"`
	Tags         []string          `json:"tags"`
	Dependencies []string          `json:"dependencies"`
	Metadata     map[string]string `json:"metadata"`
}

// ServiceContextConfig defines context configuration for a service
type ServiceContextConfig struct {
	ServiceID        string            `json:"service_id"`
	EssentialPaths   []string          `json:"essential_paths"`
	SupportingPaths  []string          `json:"supporting_paths"`
	ExcludedPatterns []string          `json:"excluded_patterns"`
	CustomBudget     *TokenBudget      `json:"custom_budget,omitempty"`
	RefreshInterval  time.Duration     `json:"refresh_interval"`
	Metadata         map[string]string `json:"metadata"`
}

// Engine is the main context orchestration engine
type Engine struct {
	vectorStore VectorStore
	embedder    EmbeddingProvider
	cache       Cache
	registry    ServiceRegistry
	config      *EngineConfig
	mu          sync.RWMutex
}

// EngineConfig contains engine configuration
type EngineConfig struct {
	DefaultBudget        *TokenBudget  `json:"default_budget"`
	CacheTTL             time.Duration `json:"cache_ttl"`
	MaxSearchResults     int           `json:"max_search_results"`
	MinRelevanceScore    float64       `json:"min_relevance_score"`
	EssentialCollection  string        `json:"essential_collection"`
	TaskCollection       string        `json:"task_collection"`
	SupportingCollection string        `json:"supporting_collection"`
	EnableCaching        bool          `json:"enable_caching"`
}

// DefaultEngineConfig returns the default engine configuration
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		DefaultBudget:        DefaultTokenBudget(),
		CacheTTL:             5 * time.Minute,
		MaxSearchResults:     100,
		MinRelevanceScore:    0.5,
		EssentialCollection:  "adp_essential",
		TaskCollection:       "adp_task_relevant",
		SupportingCollection: "adp_supporting",
		EnableCaching:        true,
	}
}

// NewEngine creates a new context orchestration engine
func NewEngine(vectorStore VectorStore, embedder EmbeddingProvider, cache Cache, registry ServiceRegistry, config *EngineConfig) *Engine {
	if config == nil {
		config = DefaultEngineConfig()
	}
	return &Engine{
		vectorStore: vectorStore,
		embedder:    embedder,
		cache:       cache,
		registry:    registry,
		config:      config,
	}
}

// AssembleContext assembles context for a request following the 4-layer hierarchy
func (e *Engine) AssembleContext(ctx context.Context, req ContextRequest) (*ContextResponse, error) {
	startTime := time.Now()

	// Validate request
	if err := e.validateRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	// Set default budget if not provided
	budget := req.TokenBudget
	if budget == nil {
		budget = e.config.DefaultBudget
	}

	// Generate cache key
	cacheKey := e.generateCacheKey(req)

	// Check cache if enabled
	if e.config.EnableCaching && e.cache != nil {
		if cached, err := e.getCachedResponse(ctx, cacheKey); err == nil && cached != nil {
			cached.CacheHit = true
			cached.RetrievalTime = time.Since(startTime)
			return cached, nil
		}
	}

	// Get service-specific configuration
	var serviceConfig *ServiceContextConfig
	if e.registry != nil && req.ServiceID != "" {
		var err error
		serviceConfig, err = e.registry.GetServiceContext(ctx, req.ServiceID)
		if err != nil && !errors.Is(err, ErrServiceNotFound) {
			return nil, fmt.Errorf("failed to get service config: %w", err)
		}
	}

	// Apply service-specific budget overrides
	if serviceConfig != nil && serviceConfig.CustomBudget != nil {
		budget = serviceConfig.CustomBudget
	}

	response := &ContextResponse{
		SessionID: req.SessionID,
		ServiceID: req.ServiceID,
		Layers:    make([]ContextLayer, 0),
		BudgetUsage: BudgetUsage{
			Essential:    TokenUsage{Available: budget.Essential},
			TaskRelevant: TokenUsage{Available: budget.TaskRelevant},
			Supporting:   TokenUsage{Available: budget.Supporting},
		},
		Metadata: ResponseMeta{
			AssembledAt:    time.Now(),
			ContextVersion: "1.0",
			CacheKey:       cacheKey,
		},
	}

	// Layer 1: Essential context (always included first)
	essentialItems, err := e.getEssentialContext(ctx, req, serviceConfig, budget.Essential)
	if err != nil {
		return nil, fmt.Errorf("failed to get essential context: %w", err)
	}
	for _, item := range essentialItems {
		layer := ContextLayer{
			Type:    LayerEssential,
			Content: item.Content,
			Tokens:  item.Tokens,
		}
		response.Layers = append(response.Layers, layer)
		response.TotalTokens += item.Tokens
		response.BudgetUsage.Essential.Used += item.Tokens
	}

	// Layer 2: Task-relevant context (semantic search based on task)
	remainingTaskBudget := budget.TaskRelevant
	if req.Task != "" {
		taskItems, err := e.getTaskRelevantContext(ctx, req, remainingTaskBudget)
		if err != nil {
			// Log but don't fail - task context is optional
			fmt.Printf("warning: failed to get task-relevant context: %v\n", err)
		} else {
			for _, item := range taskItems {
				if response.BudgetUsage.TaskRelevant.Used+item.Tokens > budget.TaskRelevant {
					continue // Skip items that would exceed budget
				}
				layer := ContextLayer{
					Type:    LayerTaskRelevant,
					Content: item.Content,
					Tokens:  item.Tokens,
				}
				response.Layers = append(response.Layers, layer)
				response.TotalTokens += item.Tokens
				response.BudgetUsage.TaskRelevant.Used += item.Tokens
			}
		}
	}

	// Layer 3: Supporting context (additional relevant context if budget allows)
	remainingBudget := budget.Supporting
	if remainingBudget > 0 && response.TotalTokens < budget.Total()-budget.Reserved {
		supportingItems, err := e.getSupportingContext(ctx, req, serviceConfig, remainingBudget)
		if err != nil {
			// Log but don't fail - supporting context is optional
			fmt.Printf("warning: failed to get supporting context: %v\n", err)
		} else {
			for _, item := range supportingItems {
				if response.BudgetUsage.Supporting.Used+item.Tokens > budget.Supporting {
					continue
				}
				layer := ContextLayer{
					Type:    LayerSupporting,
					Content: item.Content,
					Tokens:  item.Tokens,
				}
				response.Layers = append(response.Layers, layer)
				response.TotalTokens += item.Tokens
				response.BudgetUsage.Supporting.Used += item.Tokens
			}
		}
	}

	response.Metadata.SourceCount = len(response.Layers)
	response.RetrievalTime = time.Since(startTime)

	// Cache the response if enabled
	if e.config.EnableCaching && e.cache != nil {
		_ = e.cacheResponse(ctx, cacheKey, response)
	}

	return response, nil
}

// validateRequest validates a context request
func (e *Engine) validateRequest(req ContextRequest) error {
	if req.SessionID == "" {
		return errors.New("session_id is required")
	}
	if req.TokenBudget != nil {
		if req.TokenBudget.Essential < 0 || req.TokenBudget.TaskRelevant < 0 ||
			req.TokenBudget.Supporting < 0 || req.TokenBudget.Reserved < 0 {
			return errors.New("token budget values cannot be negative")
		}
	}
	return nil
}

// generateCacheKey generates a unique cache key for a request
func (e *Engine) generateCacheKey(req ContextRequest) string {
	data, _ := json.Marshal(struct {
		SessionID string
		ServiceID string
		Task      string
		Budget    *TokenBudget
	}{
		SessionID: req.SessionID,
		ServiceID: req.ServiceID,
		Task:      req.Task,
		Budget:    req.TokenBudget,
	})
	hash := sha256.Sum256(data)
	return "ctx:" + hex.EncodeToString(hash[:16])
}

// getCachedResponse retrieves a cached response
func (e *Engine) getCachedResponse(ctx context.Context, key string) (*ContextResponse, error) {
	data, err := e.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	var response ContextResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// cacheResponse caches a response
func (e *Engine) cacheResponse(ctx context.Context, key string, response *ContextResponse) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return e.cache.Set(ctx, key, data, e.config.CacheTTL)
}

// getEssentialContext retrieves essential context (constraints, ownership, incidents)
func (e *Engine) getEssentialContext(ctx context.Context, req ContextRequest, serviceConfig *ServiceContextConfig, budget int) ([]ContextItem, error) {
	items := make([]ContextItem, 0)
	usedTokens := 0

	// Get service info if available
	if e.registry != nil && req.ServiceID != "" {
		serviceInfo, err := e.registry.GetService(ctx, req.ServiceID)
		if err == nil && serviceInfo != nil {
			content := formatServiceInfo(serviceInfo)
			tokens := estimateTokens(content)
			if usedTokens+tokens <= budget {
				items = append(items, ContextItem{
					ID:        "service_info_" + req.ServiceID,
					Content:   content,
					Source:    "service_registry",
					Tokens:    tokens,
					Relevance: 1.0,
					LayerType: LayerEssential,
					Tags:      []string{"service", "ownership"},
				})
				usedTokens += tokens
			}
		}
	}

	// Search for essential content from vector store
	if e.vectorStore != nil && e.embedder != nil {
		// Build search query from service context
		searchQuery := fmt.Sprintf("essential context for service %s", req.ServiceID)
		if req.Task != "" {
			searchQuery = fmt.Sprintf("essential constraints and requirements for %s", req.Task)
		}

		embedding, err := e.embedder.Embed(ctx, searchQuery)
		if err != nil {
			return items, nil // Return what we have, don't fail
		}

		filter := map[string]interface{}{
			"layer_type": string(LayerEssential),
		}
		if req.ServiceID != "" {
			filter["service_id"] = req.ServiceID
		}

		results, err := e.vectorStore.SearchWithFilter(ctx, e.config.EssentialCollection, embedding, 20, filter)
		if err != nil {
			return items, nil
		}

		for _, result := range results {
			if result.Score < e.config.MinRelevanceScore {
				continue
			}
			content, _ := result.Payload["content"].(string)
			tokens := estimateTokens(content)
			if usedTokens+tokens > budget {
				continue
			}
			items = append(items, ContextItem{
				ID:        result.ID,
				Content:   content,
				Source:    getPayloadString(result.Payload, "source"),
				Tokens:    tokens,
				Relevance: result.Score,
				LayerType: LayerEssential,
				Tags:      getPayloadTags(result.Payload),
			})
			usedTokens += tokens
		}
	}

	return items, nil
}

// getTaskRelevantContext retrieves task-relevant context via semantic search
func (e *Engine) getTaskRelevantContext(ctx context.Context, req ContextRequest, budget int) ([]ContextItem, error) {
	if e.vectorStore == nil || e.embedder == nil {
		return nil, nil
	}

	// Generate embedding for the task description
	embedding, err := e.embedder.Embed(ctx, req.Task)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
	}

	// Build filter based on request
	filter := map[string]interface{}{}
	if req.ServiceID != "" {
		filter["service_id"] = req.ServiceID
	}
	if req.Filters != nil {
		if req.Filters.MinRelevance > 0 {
			// Relevance filtering is done post-search
		}
		if len(req.Filters.Tags) > 0 {
			filter["tags"] = req.Filters.Tags
		}
	}

	// Search for task-relevant content
	results, err := e.vectorStore.SearchWithFilter(ctx, e.config.TaskCollection, embedding, e.config.MaxSearchResults, filter)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRetrievalFailed, err)
	}

	items := make([]ContextItem, 0)
	usedTokens := 0

	// Sort by relevance score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	minRelevance := e.config.MinRelevanceScore
	if req.Filters != nil && req.Filters.MinRelevance > minRelevance {
		minRelevance = req.Filters.MinRelevance
	}

	for _, result := range results {
		if result.Score < minRelevance {
			continue
		}

		content, _ := result.Payload["content"].(string)
		tokens := estimateTokens(content)

		if usedTokens+tokens > budget {
			continue // Skip items that would exceed budget
		}

		items = append(items, ContextItem{
			ID:        result.ID,
			Content:   content,
			Source:    getPayloadString(result.Payload, "source"),
			Tokens:    tokens,
			Relevance: result.Score,
			LayerType: LayerTaskRelevant,
			Tags:      getPayloadTags(result.Payload),
			Metadata:  result.Payload,
		})
		usedTokens += tokens
	}

	return items, nil
}

// getSupportingContext retrieves supporting context (standards, dependencies, patterns)
func (e *Engine) getSupportingContext(ctx context.Context, req ContextRequest, serviceConfig *ServiceContextConfig, budget int) ([]ContextItem, error) {
	if e.vectorStore == nil || e.embedder == nil {
		return nil, nil
	}

	// Build search query for supporting context
	searchQuery := "coding standards, dependencies, and patterns"
	if req.ServiceID != "" {
		searchQuery = fmt.Sprintf("standards and patterns for service %s", req.ServiceID)
	}
	if req.Task != "" {
		searchQuery = fmt.Sprintf("best practices and patterns related to %s", req.Task)
	}

	embedding, err := e.embedder.Embed(ctx, searchQuery)
	if err != nil {
		return nil, nil // Don't fail, supporting context is optional
	}

	filter := map[string]interface{}{
		"layer_type": string(LayerSupporting),
	}
	if req.ServiceID != "" {
		filter["service_id"] = req.ServiceID
	}

	results, err := e.vectorStore.SearchWithFilter(ctx, e.config.SupportingCollection, embedding, 50, filter)
	if err != nil {
		return nil, nil
	}

	items := make([]ContextItem, 0)
	usedTokens := 0

	for _, result := range results {
		if result.Score < e.config.MinRelevanceScore {
			continue
		}

		content, _ := result.Payload["content"].(string)
		tokens := estimateTokens(content)

		if usedTokens+tokens > budget {
			continue
		}

		items = append(items, ContextItem{
			ID:        result.ID,
			Content:   content,
			Source:    getPayloadString(result.Payload, "source"),
			Tokens:    tokens,
			Relevance: result.Score,
			LayerType: LayerSupporting,
			Tags:      getPayloadTags(result.Payload),
		})
		usedTokens += tokens
	}

	return items, nil
}

// SearchContext performs a direct semantic search without layer assembly
func (e *Engine) SearchContext(ctx context.Context, query string, limit int, filters *Filters) ([]ContextItem, error) {
	if e.vectorStore == nil || e.embedder == nil {
		return nil, errors.New("vector store or embedder not configured")
	}

	embedding, err := e.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
	}

	filter := map[string]interface{}{}
	if filters != nil {
		if len(filters.Tags) > 0 {
			filter["tags"] = filters.Tags
		}
		if len(filters.Metadata) > 0 {
			for k, v := range filters.Metadata {
				filter[k] = v
			}
		}
	}

	// Search across all collections
	var allResults []SearchResult
	collections := []string{e.config.EssentialCollection, e.config.TaskCollection, e.config.SupportingCollection}
	for _, collection := range collections {
		results, err := e.vectorStore.SearchWithFilter(ctx, collection, embedding, limit, filter)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}

	// Sort by score and limit
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	items := make([]ContextItem, 0, len(allResults))
	for _, result := range allResults {
		content, _ := result.Payload["content"].(string)
		layerType := LayerType(getPayloadString(result.Payload, "layer_type"))
		items = append(items, ContextItem{
			ID:        result.ID,
			Content:   content,
			Source:    getPayloadString(result.Payload, "source"),
			Tokens:    estimateTokens(content),
			Relevance: result.Score,
			LayerType: layerType,
			Tags:      getPayloadTags(result.Payload),
			Metadata:  result.Payload,
		})
	}

	return items, nil
}

// IndexContent indexes new content into the vector store
func (e *Engine) IndexContent(ctx context.Context, items []ContextItem) error {
	if e.vectorStore == nil || e.embedder == nil {
		return errors.New("vector store or embedder not configured")
	}

	// Group items by layer type
	byLayer := make(map[LayerType][]ContextItem)
	for _, item := range items {
		byLayer[item.LayerType] = append(byLayer[item.LayerType], item)
	}

	// Index each group to the appropriate collection
	collectionMap := map[LayerType]string{
		LayerEssential:    e.config.EssentialCollection,
		LayerTaskRelevant: e.config.TaskCollection,
		LayerSupporting:   e.config.SupportingCollection,
	}

	for layerType, layerItems := range byLayer {
		collection, ok := collectionMap[layerType]
		if !ok {
			continue
		}

		// Generate embeddings
		texts := make([]string, len(layerItems))
		for i, item := range layerItems {
			texts[i] = item.Content
		}

		embeddings, err := e.embedder.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
		}

		// Prepare vector items
		vectorItems := make([]VectorItem, len(layerItems))
		for i, item := range layerItems {
			vectorItems[i] = VectorItem{
				ID:     item.ID,
				Vector: embeddings[i],
				Payload: map[string]interface{}{
					"content":    item.Content,
					"source":     item.Source,
					"tokens":     item.Tokens,
					"layer_type": string(item.LayerType),
					"tags":       item.Tags,
					"created_at": item.CreatedAt.Format(time.RFC3339),
				},
			}
			if item.Metadata != nil {
				for k, v := range item.Metadata {
					vectorItems[i].Payload[k] = v
				}
			}
		}

		// Upsert to vector store
		if err := e.vectorStore.Upsert(ctx, collection, vectorItems); err != nil {
			return fmt.Errorf("failed to index content to %s: %w", collection, err)
		}
	}

	return nil
}

// DeleteContent removes content from the vector store
func (e *Engine) DeleteContent(ctx context.Context, ids []string, layerType LayerType) error {
	if e.vectorStore == nil {
		return errors.New("vector store not configured")
	}

	collectionMap := map[LayerType]string{
		LayerEssential:    e.config.EssentialCollection,
		LayerTaskRelevant: e.config.TaskCollection,
		LayerSupporting:   e.config.SupportingCollection,
	}

	collection, ok := collectionMap[layerType]
	if !ok {
		return errors.New("invalid layer type")
	}

	return e.vectorStore.Delete(ctx, collection, ids)
}

// InvalidateCache clears the context cache for a session or service
func (e *Engine) InvalidateCache(ctx context.Context, sessionID, serviceID string) error {
	if e.cache == nil {
		return nil
	}

	// Generate and delete cache keys
	// In a production implementation, you'd want to track cache keys
	// or use a pattern-based delete
	patterns := []string{}
	if sessionID != "" {
		patterns = append(patterns, "ctx:*"+sessionID+"*")
	}
	if serviceID != "" {
		patterns = append(patterns, "ctx:*"+serviceID+"*")
	}

	for _, pattern := range patterns {
		_ = e.cache.Delete(ctx, pattern)
	}

	return nil
}

// Helper functions

func formatServiceInfo(info *ServiceInfo) string {
	return fmt.Sprintf(`# Service: %s

**Description:** %s

**Owner:** %s
**Team:** %s

**Tags:** %v

**Dependencies:** %v
`, info.Name, info.Description, info.Owner, info.Team, info.Tags, info.Dependencies)
}

// estimateTokens provides a rough token count estimation
// Uses the heuristic of ~4 characters per token for English text
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough estimation: ~4 chars per token
	return (len(text) + 3) / 4
}

func getPayloadString(payload map[string]interface{}, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func getPayloadTags(payload map[string]interface{}) []string {
	if v, ok := payload["tags"].([]interface{}); ok {
		tags := make([]string, 0, len(v))
		for _, t := range v {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
		return tags
	}
	if v, ok := payload["tags"].([]string); ok {
		return v
	}
	return nil
}
