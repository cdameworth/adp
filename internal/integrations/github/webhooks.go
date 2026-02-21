package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookHandler handles GitHub webhook events.
type WebhookHandler struct {
	app           *App
	commitChecker CommitChecker
}

// CommitChecker verifies commit audit trails.
type CommitChecker interface {
	// IsCommitVerified checks if a commit has an ADP audit trail.
	IsCommitVerified(ctx context.Context, commitSHA string) (bool, error)
	// GetCommitSession returns the session ID associated with a commit.
	GetCommitSession(ctx context.Context, commitSHA string) (string, error)
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(app *App, checker CommitChecker) *WebhookHandler {
	return &WebhookHandler{
		app:           app,
		commitChecker: checker,
	}
}

// ServeHTTP handles incoming webhook requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the payload
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read payload", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if err := h.app.VerifyWebhookSignature(payload, signature); err != nil {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Get event type
	eventType := r.Header.Get("X-GitHub-Event")

	// Process the event
	var response interface{}
	switch eventType {
	case "push":
		response, err = h.handlePushEvent(r.Context(), payload)
	case "pull_request":
		response, err = h.handlePullRequestEvent(r.Context(), payload)
	case "check_run":
		response, err = h.handleCheckRunEvent(r.Context(), payload)
	case "installation":
		response, err = h.handleInstallationEvent(r.Context(), payload)
	case "ping":
		response = map[string]string{"status": "pong"}
	default:
		// Acknowledge unknown events
		response = map[string]string{"status": "ignored", "event": eventType}
	}

	if err != nil {
		// Log error but still return 200 to avoid retries
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// PushEvent represents a GitHub push webhook payload.
type PushEvent struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Created    bool   `json:"created"`
	Deleted    bool   `json:"deleted"`
	Forced     bool   `json:"forced"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	Commits []struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
		Added    []string `json:"added"`
		Removed  []string `json:"removed"`
		Modified []string `json:"modified"`
	} `json:"commits"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// handlePushEvent processes push webhook events.
func (h *WebhookHandler) handlePushEvent(ctx context.Context, payload []byte) (interface{}, error) {
	var event PushEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("failed to parse push event: %w", err)
	}

	// Skip branch deletions
	if event.Deleted {
		return map[string]string{"status": "skipped", "reason": "branch deletion"}, nil
	}

	// Parse repo owner and name
	parts := strings.SplitN(event.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository name: %s", event.Repository.FullName)
	}
	owner, repo := parts[0], parts[1]

	// Verify each commit
	results := make([]CommitVerificationResult, 0, len(event.Commits))
	allVerified := true

	for _, commit := range event.Commits {
		verified, err := h.commitChecker.IsCommitVerified(ctx, commit.ID)
		if err != nil {
			// Treat errors as unverified
			verified = false
		}

		result := CommitVerificationResult{
			CommitSHA: commit.ID,
			Verified:  verified,
			Message:   commit.Message,
		}

		if verified {
			sessionID, _ := h.commitChecker.GetCommitSession(ctx, commit.ID)
			result.SessionID = sessionID
		} else {
			allVerified = false
			result.Reason = "No ADP audit trail found"
		}

		results = append(results, result)
	}

	// Create check run for the head commit
	headSHA := event.After
	var checkConclusion, checkTitle, checkSummary string

	if allVerified {
		checkConclusion = "success"
		checkTitle = "ADP Compliance: All commits verified"
		checkSummary = fmt.Sprintf("All %d commit(s) have valid ADP audit trails.", len(event.Commits))
	} else {
		checkConclusion = "failure"
		checkTitle = "ADP Compliance: Missing audit trails"
		unverifiedCount := 0
		for _, r := range results {
			if !r.Verified {
				unverifiedCount++
			}
		}
		checkSummary = fmt.Sprintf("%d of %d commit(s) are missing ADP audit trails.\n\n"+
			"Commits must be made with an active ADP session to pass compliance checks.",
			unverifiedCount, len(event.Commits))
	}

	_, err := h.app.CreateCheckRun(ctx, event.Installation.ID, owner, repo, headSHA, CheckRunOptions{
		Name:        "adp/compliance",
		Status:      "completed",
		Conclusion:  checkConclusion,
		Title:       checkTitle,
		Summary:     checkSummary,
		CompletedAt: time.Now(),
	})
	if err != nil {
		// Log but don't fail
		fmt.Printf("Failed to create check run: %v\n", err)
	}

	return map[string]interface{}{
		"status":       "processed",
		"all_verified": allVerified,
		"commits":      results,
	}, nil
}

// CommitVerificationResult holds the result of verifying a single commit.
type CommitVerificationResult struct {
	CommitSHA string `json:"commit_sha"`
	Verified  bool   `json:"verified"`
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// PullRequestEvent represents a GitHub pull request webhook payload.
type PullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		ID     int64  `json:"id"`
		Number int    `json:"number"`
		State  string `json:"state"`
		Title  string `json:"title"`
		Head   struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// handlePullRequestEvent processes pull request webhook events.
func (h *WebhookHandler) handlePullRequestEvent(ctx context.Context, payload []byte) (interface{}, error) {
	var event PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("failed to parse pull request event: %w", err)
	}

	// Only process opened and synchronize events
	if event.Action != "opened" && event.Action != "synchronize" {
		return map[string]string{
			"status": "skipped",
			"reason": fmt.Sprintf("action '%s' not processed", event.Action),
		}, nil
	}

	parts := strings.SplitN(event.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository name: %s", event.Repository.FullName)
	}
	owner, repo := parts[0], parts[1]
	headSHA := event.PullRequest.Head.SHA

	// Create an in-progress check run
	checkRun, err := h.app.CreateCheckRun(ctx, event.Installation.ID, owner, repo, headSHA, CheckRunOptions{
		Name:      "adp/compliance",
		Status:    "in_progress",
		Title:     "ADP Compliance Check",
		Summary:   "Verifying commit audit trails...",
		StartedAt: time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create check run: %w", err)
	}

	// Verify the head commit
	verified, err := h.commitChecker.IsCommitVerified(ctx, headSHA)
	if err != nil {
		verified = false
	}

	var conclusion, title, summary string
	if verified {
		conclusion = "success"
		title = "ADP Compliance: Verified"
		sessionID, _ := h.commitChecker.GetCommitSession(ctx, headSHA)
		summary = fmt.Sprintf("Head commit has a valid ADP audit trail.\n\nSession: `%s`", sessionID)
	} else {
		conclusion = "failure"
		title = "ADP Compliance: Not Verified"
		summary = "Head commit is missing an ADP audit trail.\n\n" +
			"Commits must be made with an active ADP session to pass compliance checks.\n\n" +
			"**To fix:**\n" +
			"1. Start an ADP session: `adp session start`\n" +
			"2. Make your changes\n" +
			"3. Commit with the ADP hooks enabled"
	}

	// Update the check run with results
	_, err = h.app.UpdateCheckRun(ctx, event.Installation.ID, owner, repo, checkRun.ID, CheckRunOptions{
		Status:      "completed",
		Conclusion:  conclusion,
		Title:       title,
		Summary:     summary,
		CompletedAt: time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update check run: %w", err)
	}

	return map[string]interface{}{
		"status":     "processed",
		"pr_number":  event.Number,
		"head_sha":   headSHA,
		"verified":   verified,
		"conclusion": conclusion,
	}, nil
}

// CheckRunEvent represents a GitHub check run webhook payload.
type CheckRunEvent struct {
	Action   string `json:"action"`
	CheckRun struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		ExternalID string `json:"external_id"`
		CheckSuite struct {
			ID int64 `json:"id"`
		} `json:"check_suite"`
	} `json:"check_run"`
	Repository struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// handleCheckRunEvent processes check run webhook events.
func (h *WebhookHandler) handleCheckRunEvent(ctx context.Context, payload []byte) (interface{}, error) {
	var event CheckRunEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("failed to parse check run event: %w", err)
	}

	// Only handle re-requested check runs
	if event.Action != "rerequested" {
		return map[string]string{
			"status": "skipped",
			"reason": fmt.Sprintf("action '%s' not processed", event.Action),
		}, nil
	}

	// Only re-run our own checks
	if event.CheckRun.Name != "adp/compliance" {
		return map[string]string{
			"status": "skipped",
			"reason": "not an ADP check run",
		}, nil
	}

	parts := strings.SplitN(event.Repository.FullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository name: %s", event.Repository.FullName)
	}
	owner, repo := parts[0], parts[1]
	headSHA := event.CheckRun.HeadSHA

	// Re-verify the commit
	verified, _ := h.commitChecker.IsCommitVerified(ctx, headSHA)

	var conclusion, title, summary string
	if verified {
		conclusion = "success"
		title = "ADP Compliance: Verified"
		sessionID, _ := h.commitChecker.GetCommitSession(ctx, headSHA)
		summary = fmt.Sprintf("Commit has a valid ADP audit trail.\n\nSession: `%s`", sessionID)
	} else {
		conclusion = "failure"
		title = "ADP Compliance: Not Verified"
		summary = "Commit is missing an ADP audit trail."
	}

	_, err := h.app.CreateCheckRun(ctx, event.Installation.ID, owner, repo, headSHA, CheckRunOptions{
		Name:        "adp/compliance",
		Status:      "completed",
		Conclusion:  conclusion,
		Title:       title,
		Summary:     summary,
		CompletedAt: time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create check run: %w", err)
	}

	return map[string]interface{}{
		"status":     "processed",
		"head_sha":   headSHA,
		"verified":   verified,
		"conclusion": conclusion,
	}, nil
}

// InstallationEvent represents a GitHub App installation webhook payload.
type InstallationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	} `json:"installation"`
	Repositories []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repositories"`
}

// handleInstallationEvent processes installation webhook events.
func (h *WebhookHandler) handleInstallationEvent(ctx context.Context, payload []byte) (interface{}, error) {
	var event InstallationEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("failed to parse installation event: %w", err)
	}

	// Log installation events for tracking
	switch event.Action {
	case "created":
		fmt.Printf("GitHub App installed for %s (installation ID: %d)\n",
			event.Installation.Account.Login, event.Installation.ID)
	case "deleted":
		fmt.Printf("GitHub App uninstalled from %s (installation ID: %d)\n",
			event.Installation.Account.Login, event.Installation.ID)
	case "suspend":
		fmt.Printf("GitHub App suspended for %s (installation ID: %d)\n",
			event.Installation.Account.Login, event.Installation.ID)
	case "unsuspend":
		fmt.Printf("GitHub App unsuspended for %s (installation ID: %d)\n",
			event.Installation.Account.Login, event.Installation.ID)
	}

	return map[string]interface{}{
		"status":          "processed",
		"action":          event.Action,
		"installation_id": event.Installation.ID,
		"account":         event.Installation.Account.Login,
		"repositories":    len(event.Repositories),
	}, nil
}
