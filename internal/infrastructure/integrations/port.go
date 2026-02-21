package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PortProvider implements CatalogProvider for Port/Cortex
type PortProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewPortProvider creates a new PortProvider
func NewPortProvider(baseURL, apiKey string) *PortProvider {
	return &PortProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetEntities fetches entities from Port
func (p *PortProvider) GetEntities(ctx context.Context) ([]Entity, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/v1/blueprints", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch entities from port: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("port returned status: %d", resp.StatusCode)
	}

	var entities []Entity
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return entities, nil
}
