package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	ctxengine "github.com/adp/adp/internal/domain/context"
	"github.com/adp/adp/internal/store"
)

// Tool input types

type StartSessionArgs struct {
	OrganizationID string   `json:"organization_id"`
	UserID         string   `json:"user_id"`
	Tool           string   `json:"tool"`
	TrustLevel     int      `json:"trust_level"`
	Capabilities   []string `json:"capabilities"`
	ServiceScope   []string `json:"service_scope"`
}

type EndSessionArgs struct {
	SessionID string `json:"session_id"`
}

type HeartbeatArgs struct {
	SessionID string `json:"session_id"`
}

type GetContextArgs struct {
	SessionID   string `json:"session_id"`
	ServiceID   string `json:"service_id"`
	Task        string `json:"task"`
	TokenBudget struct {
		Essential    int `json:"essential"`
		TaskRelevant int `json:"task_relevant"`
		Supporting   int `json:"supporting"`
	} `json:"token_budget"`
}

type CheckActionArgs struct {
	SessionID  string                 `json:"session_id"`
	ActionType string                 `json:"action_type"`
	Target     map[string]interface{} `json:"target"`
	Context    map[string]interface{} `json:"context"`
}

type RequestApprovalArgs struct {
	SessionID  string                 `json:"session_id"`
	Action     string                 `json:"action"`
	ActionType string                 `json:"action_type"`
	Target     map[string]interface{} `json:"target"`
	Reason     string                 `json:"reason"`
	Priority   string                 `json:"priority"`
}

type GetApprovalArgs struct {
	ApprovalID string `json:"approval_id"`
}

type LogDecisionArgs struct {
	SessionID    string                 `json:"session_id"`
	DecisionType string                 `json:"decision_type"`
	Action       string                 `json:"action"`
	Target       map[string]interface{} `json:"target"`
	Reasoning    map[string]interface{} `json:"reasoning"`
	Confidence   float64                `json:"confidence"`
	Alternatives []struct {
		Action     string  `json:"action"`
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
	} `json:"alternatives"`
}

type PrepareCommitArgs struct {
	SessionID string   `json:"session_id"`
	Files     []string `json:"files"`
	Message   string   `json:"message"`
}

type VerifyCommitArgs struct {
	CommitSHA string `json:"commit_sha"`
}

type GetDocsArgs struct {
	Category  string `json:"category"`
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	Limit     int    `json:"limit"`
}

// Tool result types

type StartSessionResult struct {
	SessionID    string    `json:"session_id"`
	SessionToken string    `json:"session_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TrustLevel   int       `json:"trust_level"`
	HTTPPort     int       `json:"http_port"`
	Capabilities []string  `json:"capabilities"`
	Constraints  []string  `json:"constraints"`
}

type GetContextResult struct {
	Essential    ContextLayer `json:"essential"`
	TaskRelevant ContextLayer `json:"task_relevant"`
	Supporting   ContextLayer `json:"supporting"`
	TokensUsed   int          `json:"tokens_used"`
}

type ContextLayer struct {
	Content    string `json:"content"`
	TokenCount int    `json:"token_count"`
}

type CheckActionResult struct {
	Allowed          bool     `json:"allowed"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	DeniedReasons    []string `json:"denied_reasons,omitempty"`
	PolicyNames      []string `json:"policy_names"`
	Restrictions     []string `json:"restrictions,omitempty"`
}

type RequestApprovalResult struct {
	ApprovalID string     `json:"approval_id"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type GetApprovalResult struct {
	ApprovalID      string     `json:"approval_id"`
	Status          string     `json:"status"`
	ApproverComment string     `json:"approver_comment,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}

type LogDecisionResult struct {
	DecisionID string    `json:"decision_id"`
	RecordedAt time.Time `json:"recorded_at"`
}

type PrepareCommitResult struct {
	CommitToken string `json:"commit_token"`
	Approved    bool   `json:"approved"`
	Reason      string `json:"reason,omitempty"`
}

type VerifyCommitResult struct {
	Verified   bool   `json:"verified"`
	SessionID  string `json:"session_id,omitempty"`
	VerifiedAt string `json:"verified_at,omitempty"`
}

// ---------- Tool handlers ----------

func (s *Server) handleStartSession(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input StartSessionArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if input.OrganizationID == "" {
		return nil, fmt.Errorf("organization_id is required")
	}
	if input.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if input.Tool == "" {
		return nil, fmt.Errorf("tool is required")
	}
	if input.TrustLevel == 0 {
		input.TrustLevel = 2
	}
	if input.TrustLevel < 1 || input.TrustLevel > 5 {
		return nil, fmt.Errorf("trust_level must be between 1 and 5")
	}

	sessionID := generateSessionID()
	expiresAt := time.Now().Add(8 * time.Hour)

	var constraints []string
	switch input.TrustLevel {
	case 1:
		constraints = []string{"read_only", "no_file_modifications"}
	case 2:
		constraints = []string{"require_review_for_delete", "max_files_per_commit:10"}
	case 3:
		constraints = []string{"max_files_per_commit:50"}
	default:
		constraints = []string{}
	}

	// Generate a cryptographically random session token
	sessionToken := generateSessionToken()
	tokenHash := sha256Hex(sessionToken)

	if s.SessionStore != nil {
		session, err := s.SessionStore.Create(ctx, store.CreateSessionInput{
			ID:             sessionID,
			OrganizationID: input.OrganizationID,
			UserID:         input.UserID,
			Tool:           input.Tool,
			TrustLevel:     input.TrustLevel,
			Capabilities:   input.Capabilities,
			Constraints:    constraints,
			ServiceScope:   input.ServiceScope,
			ExpiresAt:      expiresAt,
			TokenHash:      tokenHash,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
		return jsonResult(StartSessionResult{
			SessionID:    session.ID,
			SessionToken: sessionToken,
			ExpiresAt:    session.ExpiresAt,
			TrustLevel:   session.TrustLevel,
			HTTPPort:     s.HTTPPort,
			Capabilities: session.Capabilities,
			Constraints:  session.Constraints,
		}), nil
	}

	return jsonResult(StartSessionResult{
		SessionID:    sessionID,
		SessionToken: sessionToken,
		ExpiresAt:    expiresAt,
		TrustLevel:   input.TrustLevel,
		HTTPPort:     s.HTTPPort,
		Capabilities: input.Capabilities,
		Constraints:  constraints,
	}), nil
}

func (s *Server) handleEndSession(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input EndSessionArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	if s.SessionStore != nil {
		if err := s.SessionStore.End(ctx, input.SessionID); err != nil {
			return nil, fmt.Errorf("failed to end session: %w", err)
		}
	}

	return textResult(fmt.Sprintf("Session %s ended successfully", input.SessionID)), nil
}

func (s *Server) handleHeartbeat(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input HeartbeatArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	if s.SessionStore != nil {
		if err := s.SessionStore.Heartbeat(ctx, input.SessionID); err != nil {
			return nil, fmt.Errorf("failed to update heartbeat: %w", err)
		}
	}

	return jsonResult(map[string]interface{}{
		"session_id":     input.SessionID,
		"status":         "active",
		"last_heartbeat": time.Now().Format(time.RFC3339),
	}), nil
}

func (s *Server) handleGetContext(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input GetContextArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if input.Task == "" {
		return nil, fmt.Errorf("task is required")
	}

	// Use context engine if available
	if s.ContextEngine != nil {
		req := &ctxengine.ContextRequest{
			SessionID: input.SessionID,
			ServiceID: input.ServiceID,
			Task:      input.Task,
		}
		if input.TokenBudget.Essential > 0 || input.TokenBudget.TaskRelevant > 0 || input.TokenBudget.Supporting > 0 {
			req.TokenBudget = &ctxengine.TokenBudget{
				Essential:    input.TokenBudget.Essential,
				TaskRelevant: input.TokenBudget.TaskRelevant,
				Supporting:   input.TokenBudget.Supporting,
			}
		}

		resp, err := s.ContextEngine.AssembleContext(ctx, req)
		if err != nil {
			// Fall through to defaults on engine error
			fmt.Printf("warning: context engine error, using defaults: %v\n", err)
		} else if contextResp, ok := resp.(*ctxengine.ContextResponse); ok {
			return jsonResult(convertContextResponse(contextResp, input.Task)), nil
		}
	}

	// Fallback: return basic context when engine is not configured or errored
	result := GetContextResult{
		Essential: ContextLayer{
			Content:    "# Service Constraints\n- Follow existing code style\n- All changes require tests\n- No direct database queries outside repository layer",
			TokenCount: 50,
		},
		TaskRelevant: ContextLayer{
			Content:    fmt.Sprintf("# Task Context\nTask: %s\n\nNo context engine configured. Connect a vector store for semantic context retrieval.", input.Task),
			TokenCount: 100,
		},
		Supporting: ContextLayer{
			Content:    "# Supporting Information\nAdditional patterns, dependencies, and standards documentation.",
			TokenCount: 30,
		},
		TokensUsed: 180,
	}

	return jsonResult(result), nil
}

// convertContextResponse maps the domain ContextResponse to the MCP GetContextResult
func convertContextResponse(resp *ctxengine.ContextResponse, task string) GetContextResult {
	result := GetContextResult{
		TokensUsed: resp.TotalTokens,
	}

	// Aggregate content by layer type
	var essentialContent, taskContent, supportingContent string
	var essentialTokens, taskTokens, supportingTokens int

	for _, layer := range resp.Layers {
		switch layer.Type {
		case ctxengine.LayerEssential:
			if essentialContent != "" {
				essentialContent += "\n\n"
			}
			essentialContent += layer.Content
			essentialTokens += layer.Tokens
		case ctxengine.LayerTaskRelevant:
			if taskContent != "" {
				taskContent += "\n\n"
			}
			taskContent += layer.Content
			taskTokens += layer.Tokens
		case ctxengine.LayerSupporting:
			if supportingContent != "" {
				supportingContent += "\n\n"
			}
			supportingContent += layer.Content
			supportingTokens += layer.Tokens
		}
	}

	// Use defaults for empty layers
	if essentialContent == "" {
		essentialContent = "# Service Constraints\n- Follow existing code style\n- All changes require tests"
		essentialTokens = 30
	}
	if taskContent == "" {
		taskContent = fmt.Sprintf("# Task Context\nTask: %s\n\nNo task-relevant context found. Configure a vector store and embedding provider for semantic retrieval.", task)
		taskTokens = 50
	}

	result.Essential = ContextLayer{Content: essentialContent, TokenCount: essentialTokens}
	result.TaskRelevant = ContextLayer{Content: taskContent, TokenCount: taskTokens}
	result.Supporting = ContextLayer{Content: supportingContent, TokenCount: supportingTokens}

	if result.TokensUsed == 0 {
		result.TokensUsed = essentialTokens + taskTokens + supportingTokens
	}

	return result
}

func (s *Server) handleCheckAction(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input CheckActionArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if input.ActionType == "" {
		return nil, fmt.Errorf("action_type is required")
	}

	var paths []string
	if p, ok := input.Target["paths"].([]interface{}); ok {
		for _, path := range p {
			if str, ok := path.(string); ok {
				paths = append(paths, str)
			}
		}
	}
	if p, ok := input.Target["path"].(string); ok {
		paths = append(paths, p)
	}

	var services []string
	if svcList, ok := input.Target["services"].([]interface{}); ok {
		for _, svc := range svcList {
			if str, ok := svc.(string); ok {
				services = append(services, str)
			}
		}
	}

	environment := ""
	if env, ok := input.Target["environment"].(string); ok {
		environment = env
	}
	if input.Context != nil {
		if env, ok := input.Context["environment"].(string); ok {
			environment = env
		}
	}

	trustLevel := 2
	if input.Context != nil {
		if tl, ok := input.Context["trust_level"].(float64); ok {
			trustLevel = int(tl)
		}
	}

	hour := time.Now().Hour()
	if input.Context != nil {
		if h, ok := input.Context["hour"].(float64); ok {
			hour = int(h)
		}
	}

	// Use unified policy engine if available
	if s.UnifiedPolicyEngine != nil {
		evalInput := &UnifiedEvalInput{
			SessionID:  input.SessionID,
			TrustLevel: trustLevel,
			Action: UnifiedActionInput{
				Type: input.ActionType,
				Target: UnifiedTargetInput{
					Paths:       paths,
					Services:    services,
					Environment: environment,
				},
				Metadata: input.Target,
			},
			Context: UnifiedContextInput{
				Environment: environment,
				Hour:        hour,
			},
			Session: UnifiedSessionInput{
				TrustLevel: trustLevel,
			},
		}

		evalResult, err := s.UnifiedPolicyEngine.Evaluate(ctx, evalInput)
		if err != nil {
			return jsonResult(CheckActionResult{
				Allowed:          false,
				RequiresApproval: true,
				DeniedReasons:    []string{"Policy evaluation error: " + err.Error()},
				PolicyNames:      []string{},
			}), nil
		}

		result := CheckActionResult{
			Allowed:          evalResult.Allowed,
			RequiresApproval: evalResult.RequiresApproval,
			DeniedReasons:    evalResult.DeniedReasons,
			PolicyNames:      evalResult.MatchedPolicies,
		}
		if environment == "production" {
			result.Restrictions = append(result.Restrictions, "production_environment_restrictions")
		}

		return jsonResult(result), nil
	}

	// Fallback to hardcoded checks
	result := CheckActionResult{
		Allowed:     true,
		PolicyNames: []string{"default_policy"},
	}

	for _, path := range paths {
		if isSensitivePath(path) {
			result.Allowed = false
			result.RequiresApproval = true
			result.DeniedReasons = append(result.DeniedReasons,
				fmt.Sprintf("Sensitive path requires approval: %s", path))
			result.PolicyNames = append(result.PolicyNames, "sensitive_paths_policy")
		}
	}

	if environment == "production" {
		result.Restrictions = append(result.Restrictions, "production_environment_restrictions")
		if input.ActionType == "deploy" {
			result.Allowed = false
			result.RequiresApproval = true
			result.DeniedReasons = append(result.DeniedReasons,
				"Production deployments require human approval")
			result.PolicyNames = append(result.PolicyNames, "production_deploy_policy")
		}
	}

	return jsonResult(result), nil
}

func (s *Server) handleRequestApproval(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input RequestApprovalArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if input.Action == "" {
		return nil, fmt.Errorf("action is required")
	}
	if input.Reason == "" {
		return nil, fmt.Errorf("reason is required")
	}
	if input.Priority == "" {
		input.Priority = "normal"
	}

	var expiresAt time.Time
	switch input.Priority {
	case "critical":
		expiresAt = time.Now().Add(15 * time.Minute)
	case "high":
		expiresAt = time.Now().Add(30 * time.Minute)
	case "normal":
		expiresAt = time.Now().Add(1 * time.Hour)
	case "low":
		expiresAt = time.Now().Add(4 * time.Hour)
	}

	if s.EscalationStore != nil {
		escalation, err := s.EscalationStore.Create(ctx, store.CreateEscalationInput{
			SessionID:  input.SessionID,
			Action:     input.Action,
			ActionType: input.ActionType,
			Target:     input.Target,
			Reason:     input.Reason,
			Priority:   input.Priority,
			ExpiresAt:  &expiresAt,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create escalation: %w", err)
		}
		return jsonResult(RequestApprovalResult{
			ApprovalID: escalation.ID,
			Status:     escalation.Status,
			ExpiresAt:  escalation.ExpiresAt,
		}), nil
	}

	return jsonResult(RequestApprovalResult{
		ApprovalID: generateSessionID(),
		Status:     "pending",
		ExpiresAt:  &expiresAt,
	}), nil
}

func (s *Server) handleGetApproval(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input GetApprovalArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.ApprovalID == "" {
		return nil, fmt.Errorf("approval_id is required")
	}

	if s.EscalationStore != nil {
		escalation, err := s.EscalationStore.Get(ctx, input.ApprovalID)
		if err != nil {
			return nil, fmt.Errorf("failed to get escalation: %w", err)
		}
		return jsonResult(GetApprovalResult{
			ApprovalID:      escalation.ID,
			Status:          escalation.Status,
			ApproverComment: escalation.ApproverComment,
			ResolvedAt:      escalation.ResolvedAt,
		}), nil
	}

	return jsonResult(GetApprovalResult{
		ApprovalID: input.ApprovalID,
		Status:     "pending",
	}), nil
}

func (s *Server) handleLogDecision(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input LogDecisionArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if input.DecisionType == "" {
		return nil, fmt.Errorf("decision_type is required")
	}
	if input.Action == "" {
		return nil, fmt.Errorf("action is required")
	}
	if input.Confidence == 0 {
		input.Confidence = 0.8
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return nil, fmt.Errorf("confidence must be between 0 and 1")
	}

	var alts []store.Alternative
	for _, a := range input.Alternatives {
		alts = append(alts, store.Alternative{
			Action:     a.Action,
			Reason:     a.Reason,
			Confidence: a.Confidence,
		})
	}

	if s.DecisionStore != nil {
		record, err := s.DecisionStore.Create(ctx, store.CreateDecisionInput{
			SessionID:    input.SessionID,
			DecisionType: input.DecisionType,
			Action:       input.Action,
			Target:       input.Target,
			Reasoning:    input.Reasoning,
			Confidence:   input.Confidence,
			Alternatives: alts,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to log decision: %w", err)
		}
		return jsonResult(LogDecisionResult{
			DecisionID: record.ID,
			RecordedAt: record.CreatedAt,
		}), nil
	}

	return jsonResult(LogDecisionResult{
		DecisionID: generateSessionID(),
		RecordedAt: time.Now(),
	}), nil
}

func (s *Server) handlePrepareCommit(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input PrepareCommitArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if len(input.Files) == 0 {
		return nil, fmt.Errorf("files are required")
	}

	// Check sensitive files as a baseline.
	var deniedFiles []string
	for _, file := range input.Files {
		if isSensitivePath(file) {
			deniedFiles = append(deniedFiles, file)
		}
	}

	// Evaluate against the unified policy engine if available.
	var policyDenied bool
	var policyReason string
	var policyRequiresApproval bool
	if s.UnifiedPolicyEngine != nil {
		evalInput := &UnifiedEvalInput{
			SessionID: input.SessionID,
			Action: UnifiedActionInput{
				Type: "commit",
				Target: UnifiedTargetInput{
					Paths: input.Files,
				},
				Metadata: map[string]interface{}{
					"message": input.Message,
					"files":   input.Files,
				},
			},
		}

		evalResult, err := s.UnifiedPolicyEngine.Evaluate(ctx, evalInput)
		if err != nil {
			return jsonResult(PrepareCommitResult{
				Approved: false,
				Reason:   "Policy evaluation error: " + err.Error(),
			}), nil
		}

		if !evalResult.Allowed {
			policyDenied = true
			if len(evalResult.DeniedReasons) > 0 {
				policyReason = fmt.Sprintf("Policy denied: %v", evalResult.DeniedReasons)
			}
		}
		if evalResult.RequiresApproval {
			policyRequiresApproval = true
		}
	}

	// Determine overall approval.
	approved := len(deniedFiles) == 0 && !policyDenied && !policyRequiresApproval
	var reason string
	switch {
	case len(deniedFiles) > 0:
		reason = fmt.Sprintf("Sensitive files require approval: %v", deniedFiles)
	case policyDenied:
		reason = policyReason
	case policyRequiresApproval:
		reason = "Policy requires human approval for this commit"
	}

	if s.CommitStore != nil {
		record, err := s.CommitStore.Prepare(ctx, store.PrepareCommitInput{
			SessionID: input.SessionID,
			Files:     input.Files,
			Message:   input.Message,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to prepare commit: %w", err)
		}
		result := PrepareCommitResult{
			CommitToken: record.CommitToken,
			Approved:    approved,
			Reason:      reason,
		}
		return jsonResult(result), nil
	}

	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	result := PrepareCommitResult{
		CommitToken: "adp_" + hex.EncodeToString(tokenBytes),
		Approved:    approved,
		Reason:      reason,
	}
	return jsonResult(result), nil
}

func (s *Server) handleVerifyCommit(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input VerifyCommitArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.CommitSHA == "" {
		return nil, fmt.Errorf("commit_sha is required")
	}

	if s.CommitStore != nil {
		verified, err := s.CommitStore.IsCommitVerified(ctx, input.CommitSHA)
		if err != nil {
			return nil, fmt.Errorf("failed to verify commit: %w", err)
		}
		result := VerifyCommitResult{Verified: verified}
		if verified {
			result.VerifiedAt = time.Now().Format(time.RFC3339)
		}
		return jsonResult(result), nil
	}

	return jsonResult(VerifyCommitResult{Verified: false}), nil
}

func (s *Server) handleGetDocs(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var input GetDocsArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if input.Limit <= 0 {
		input.Limit = 10
	}

	if s.DocStore == nil {
		return textResult("Documentation store not configured"), nil
	}

	var docs []*store.DocRecord
	var err error

	switch {
	case input.SessionID != "":
		docs, err = s.DocStore.ListBySession(ctx, input.SessionID)
	case input.Category != "":
		docs, err = s.DocStore.ListByCategory(ctx, input.Category, input.Limit)
	case input.Query != "":
		docs, err = s.DocStore.Search(ctx, input.Query, input.Limit)
	default:
		docs, err = s.DocStore.ListByCategory(ctx, "session_summary", input.Limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query docs: %w", err)
	}

	return jsonResult(docs), nil
}

// ---------- Helper functions ----------

func generateSessionID() string {
	timestamp := time.Now().Unix()
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	return fmt.Sprintf("adp_%d_%s", timestamp, hex.EncodeToString(randomBytes))
}

func generateSessionToken() string {
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	return "adp_tok_" + hex.EncodeToString(tokenBytes)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func isSensitivePath(path string) bool {
	sensitivePaths := []string{
		".env", ".secrets", "credentials", "secrets.yaml", "secrets.json",
		".aws", ".ssh", "id_rsa", "private_key", "service_account",
	}
	for _, sensitive := range sensitivePaths {
		if containsPattern(path, sensitive) {
			return true
		}
	}

	// Extension-based checks for certificate/key files
	sensitiveExtensions := []string{".pem", ".key"}
	for _, ext := range sensitiveExtensions {
		if len(path) >= len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}

	return false
}

func containsPattern(path, pattern string) bool {
	if len(path) < len(pattern) {
		return false
	}
	if path == pattern {
		return true
	}
	if len(path) > len(pattern) && path[:len(pattern)] == pattern {
		next := path[len(pattern)]
		if next == '/' || next == '.' || next == '-' || next == '_' {
			return true
		}
	}
	if containsSubstring(path, "/"+pattern) {
		return true
	}
	if containsSubstring(path, pattern+"/") {
		return true
	}
	return false
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
