package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CatalogProvider defines the interface for fetching service catalog entities
type CatalogProvider interface {
	GetEntities(ctx context.Context) ([]Entity, error)
}

// Entity represents a generic catalog entity
type Entity struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   map[string]interface{} `json:"metadata"`
	Spec       map[string]interface{} `json:"spec"`
}

// BackstageProvider implements CatalogProvider for Backstage
type BackstageProvider struct {
	baseURL string
	client  *http.Client
}

// NewBackstageProvider creates a new BackstageProvider
func NewBackstageProvider(baseURL string) *BackstageProvider {
	return &BackstageProvider{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetEntities fetches entities from Backstage
func (p *BackstageProvider) GetEntities(ctx context.Context) ([]Entity, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/catalog/entities", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch entities from backstage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backstage returned status: %d", resp.StatusCode)
	}

	var entities []Entity
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return entities, nil
}
