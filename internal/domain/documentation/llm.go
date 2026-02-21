package docengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// LLMClient generates refined documentation from a template draft and session analysis.
// When configured, the LLM takes the template-generated Markdown and produces
// curated, human-quality prose documentation.
type LLMClient interface {
	// GenerateDoc refines a template-generated draft into curated documentation.
	// The prompt provides context; data provides structured session analysis.
	// Returns refined Markdown content, or empty string to fall through to template output.
	GenerateDoc(ctx context.Context, prompt string, draft string, data SessionAnalysis) (string, error)

	// IsConfigured returns true if the LLM client has valid credentials.
	IsConfigured() bool
}

// NoopLLMClient is the default LLM client that does nothing.
// Used when no LLM API key is configured; the doc agent falls through to template output.
type NoopLLMClient struct{}

func (n *NoopLLMClient) GenerateDoc(_ context.Context, _ string, _ string, _ SessionAnalysis) (string, error) {
	return "", nil
}

func (n *NoopLLMClient) IsConfigured() bool {
	return false
}

// AnthropicClient calls the Anthropic Messages API to refine template-generated
// documentation into polished prose. Optional -- only active when ADP_DOC_LLM_API_KEY
// is set. Falls through to template output on any error.
type AnthropicClient struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// anthropicRequest is the Anthropic Messages API request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in the conversation.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the Anthropic Messages API response body.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *anthropicError         `json:"error,omitempty"`
}

// anthropicContentBlock is a content block in the response.
type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicError is an error from the Anthropic API.
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *AnthropicClient) IsConfigured() bool {
	return c.apiKey != ""
}

func (c *AnthropicClient) GenerateDoc(ctx context.Context, prompt string, draft string, data SessionAnalysis) (string, error) {
	if !c.IsConfigured() {
		return "", nil
	}

	systemPrompt := `You are a technical documentation writer for an AI agent governance system.
You receive template-generated Markdown reports about agent coding sessions and refine them
into clear, concise, professional technical documentation.

Rules:
- Preserve all factual data (numbers, file paths, metrics) exactly as provided
- Improve readability, structure, and prose quality
- Add brief interpretive context where it helps the reader understand significance
- Keep the Markdown format
- Do not invent data or add information not present in the draft
- Be concise -- shorter is better than longer
- Output only the refined Markdown, no preamble or explanation`

	userMessage := fmt.Sprintf("%s\n\n---\n\n## Template Draft\n\n%s\n\n---\n\n## Session Data\n\n"+
		"- Session: %s\n- Agent: %s\n- Trust Level: %d\n- Decisions: %d\n"+
		"- Avg Confidence: %.2f\n- Min Confidence: %.2f\n- Policy Violations: %d\n- Files Touched: %d",
		prompt, draft,
		data.SessionID, data.Tool, data.TrustLevel, data.DecisionCount,
		data.AvgConfidence, data.MinConfidence, data.PolicyViolations, len(data.FilesTouched),
	)

	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userMessage},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("api error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}

	// Extract text from content blocks
	var result string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}

	return result, nil
}

// NewLLMClient creates an LLM client based on environment configuration.
// When apiKey is empty, returns NoopLLMClient (template output is the final output).
// When apiKey is provided, returns AnthropicClient that calls the Claude API.
func NewLLMClient(apiKey string, model string) LLMClient {
	if apiKey == "" {
		return &NoopLLMClient{}
	}

	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	return &AnthropicClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}
