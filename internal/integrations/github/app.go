// Package github provides GitHub App integration for ADP.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Common errors
var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrInvalidPayload   = errors.New("invalid webhook payload")
	ErrAppNotConfigured = errors.New("GitHub App not configured")
)

// AppConfig holds the GitHub App configuration.
type AppConfig struct {
	AppID          int64  `json:"app_id"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	WebhookSecret  string `json:"webhook_secret"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	InstallationID int64  `json:"installation_id,omitempty"`
}

// App represents a GitHub App client.
type App struct {
	config     AppConfig
	httpClient *http.Client

	tokenCache   map[int64]*InstallationToken
	tokenCacheMu sync.RWMutex
}

// InstallationToken represents a GitHub App installation token.
type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NewApp creates a new GitHub App client.
func NewApp(config AppConfig) (*App, error) {
	if config.AppID == 0 {
		return nil, ErrAppNotConfigured
	}
	if config.PrivateKeyPEM == "" {
		return nil, fmt.Errorf("%w: missing private key", ErrAppNotConfigured)
	}

	return &App{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		tokenCache: make(map[int64]*InstallationToken),
	}, nil
}

// GenerateJWT creates a JWT for authenticating as the GitHub App.
func (a *App) GenerateJWT() (string, error) {
	now := time.Now()

	// Parse the private key
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(a.config.PrivateKeyPEM))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create the JWT claims
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // Issued at (allow clock drift)
		"exp": now.Add(10 * time.Minute).Unix(),  // Expires in 10 minutes (max allowed)
		"iss": strconv.FormatInt(a.config.AppID, 10),
	}

	// Create and sign the token
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signedToken, nil
}

// GetInstallationToken retrieves or refreshes an installation access token.
func (a *App) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
	// Check cache first
	a.tokenCacheMu.RLock()
	cached, exists := a.tokenCache[installationID]
	a.tokenCacheMu.RUnlock()

	if exists && cached.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		return cached.Token, nil
	}

	// Generate new token
	appJWT, err := a.GenerateJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Cache the token
	a.tokenCacheMu.Lock()
	a.tokenCache[installationID] = &InstallationToken{
		Token:     tokenResp.Token,
		ExpiresAt: tokenResp.ExpiresAt,
	}
	a.tokenCacheMu.Unlock()

	return tokenResp.Token, nil
}

// VerifyWebhookSignature validates the webhook signature from GitHub.
func (a *App) VerifyWebhookSignature(payload []byte, signature string) error {
	if a.config.WebhookSecret == "" {
		return nil // Skip verification if no secret configured
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return ErrInvalidSignature
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, []byte(a.config.WebhookSecret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return ErrInvalidSignature
	}

	return nil
}

// CreateCheckRun creates a check run on a commit.
func (a *App) CreateCheckRun(ctx context.Context, installationID int64, owner, repo, sha string, opts CheckRunOptions) (*CheckRun, error) {
	token, err := a.GetInstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{
		"name":     opts.Name,
		"head_sha": sha,
		"status":   opts.Status,
	}

	if opts.Conclusion != "" {
		payload["conclusion"] = opts.Conclusion
	}
	if opts.Title != "" || opts.Summary != "" {
		payload["output"] = map[string]string{
			"title":   opts.Title,
			"summary": opts.Summary,
		}
	}
	if opts.DetailsURL != "" {
		payload["details_url"] = opts.DetailsURL
	}
	if opts.ExternalID != "" {
		payload["external_id"] = opts.ExternalID
	}
	if !opts.StartedAt.IsZero() {
		payload["started_at"] = opts.StartedAt.Format(time.RFC3339)
	}
	if !opts.CompletedAt.IsZero() {
		payload["completed_at"] = opts.CompletedAt.Format(time.RFC3339)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs", owner, repo)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create check run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var checkRun CheckRun
	if err := json.NewDecoder(resp.Body).Decode(&checkRun); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &checkRun, nil
}

// UpdateCheckRun updates an existing check run.
func (a *App) UpdateCheckRun(ctx context.Context, installationID int64, owner, repo string, checkRunID int64, opts CheckRunOptions) (*CheckRun, error) {
	token, err := a.GetInstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	payload := map[string]interface{}{}

	if opts.Status != "" {
		payload["status"] = opts.Status
	}
	if opts.Conclusion != "" {
		payload["conclusion"] = opts.Conclusion
	}
	if opts.Title != "" || opts.Summary != "" {
		payload["output"] = map[string]string{
			"title":   opts.Title,
			"summary": opts.Summary,
		}
	}
	if !opts.CompletedAt.IsZero() {
		payload["completed_at"] = opts.CompletedAt.Format(time.RFC3339)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs/%d", owner, repo, checkRunID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to update check run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var checkRun CheckRun
	if err := json.NewDecoder(resp.Body).Decode(&checkRun); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &checkRun, nil
}

// CreateCommitStatus creates a commit status.
func (a *App) CreateCommitStatus(ctx context.Context, installationID int64, owner, repo, sha string, opts StatusOptions) error {
	token, err := a.GetInstallationToken(ctx, installationID)
	if err != nil {
		return err
	}

	payload := map[string]string{
		"state":   opts.State,
		"context": opts.Context,
	}
	if opts.TargetURL != "" {
		payload["target_url"] = opts.TargetURL
	}
	if opts.Description != "" {
		payload["description"] = opts.Description
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s", owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CheckRunOptions configures a check run.
type CheckRunOptions struct {
	Name        string
	Status      string // queued, in_progress, completed
	Conclusion  string // action_required, cancelled, failure, neutral, success, skipped, stale, timed_out
	Title       string
	Summary     string
	DetailsURL  string
	ExternalID  string
	StartedAt   time.Time
	CompletedAt time.Time
}

// CheckRun represents a GitHub check run.
type CheckRun struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	HeadSHA     string    `json:"head_sha"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url"`
}

// StatusOptions configures a commit status.
type StatusOptions struct {
	State       string // error, failure, pending, success
	Context     string
	Description string
	TargetURL   string
}
