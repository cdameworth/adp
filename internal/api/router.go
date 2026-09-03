// Package api provides the HTTP API for ADP.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/adp/adp/internal/domain/enforcement"
	"github.com/adp/adp/internal/store"
)

// CommitVerifier reports whether a commit SHA has a verified ADP governance
// trail. Both the SQLite and PostgreSQL commit stores satisfy it (it asks only
// for verification, avoiding the stores' divergent Prepare/Register signatures).
type CommitVerifier interface {
	IsCommitVerified(ctx context.Context, sha string) (bool, error)
}

// Handler interfaces for dependency injection
type SessionHandler interface {
	CreateSession(w http.ResponseWriter, r *http.Request)
	GetSession(w http.ResponseWriter, r *http.Request)
	ListSessions(w http.ResponseWriter, r *http.Request)
	UpdateSession(w http.ResponseWriter, r *http.Request)
	EndSession(w http.ResponseWriter, r *http.Request)
	Heartbeat(w http.ResponseWriter, r *http.Request)
}

type GovernanceHandler interface {
	CheckAction(w http.ResponseWriter, r *http.Request)
	RequestApproval(w http.ResponseWriter, r *http.Request)
	GetApproval(w http.ResponseWriter, r *http.Request)
	ListApprovals(w http.ResponseWriter, r *http.Request)
	ListPendingApprovals(w http.ResponseWriter, r *http.Request)
	ResolveApproval(w http.ResponseWriter, r *http.Request)
}

type AuditHandler interface {
	LogDecision(w http.ResponseWriter, r *http.Request)
	GetDecision(w http.ResponseWriter, r *http.Request)
	ListDecisions(w http.ResponseWriter, r *http.Request)
	GetLineage(w http.ResponseWriter, r *http.Request)
	PrepareCommit(w http.ResponseWriter, r *http.Request)
	RegisterCommit(w http.ResponseWriter, r *http.Request)
	VerifyCommit(w http.ResponseWriter, r *http.Request)
}

type ServiceHandler interface {
	CreateService(w http.ResponseWriter, r *http.Request)
	GetService(w http.ResponseWriter, r *http.Request)
	ListServices(w http.ResponseWriter, r *http.Request)
	UpdateService(w http.ResponseWriter, r *http.Request)
	DeleteService(w http.ResponseWriter, r *http.Request)
}

type TenantHandler interface {
	// Tenant management
	CreateTenant(w http.ResponseWriter, r *http.Request)
	GetTenant(w http.ResponseWriter, r *http.Request)
	ListTenants(w http.ResponseWriter, r *http.Request)
	UpdateTenant(w http.ResponseWriter, r *http.Request)
	// Organization management
	CreateOrganization(w http.ResponseWriter, r *http.Request)
	GetOrganization(w http.ResponseWriter, r *http.Request)
	ListOrganizations(w http.ResponseWriter, r *http.Request)
	// Team management
	CreateTeam(w http.ResponseWriter, r *http.Request)
	GetTeam(w http.ResponseWriter, r *http.Request)
	ListTeams(w http.ResponseWriter, r *http.Request)
	AddTeamMember(w http.ResponseWriter, r *http.Request)
	RemoveTeamMember(w http.ResponseWriter, r *http.Request)
	GetUserTeams(w http.ResponseWriter, r *http.Request)
}

type ReportsHandler interface {
	GetSummary(w http.ResponseWriter, r *http.Request)
	GetAdoption(w http.ResponseWriter, r *http.Request)
	GetGovernance(w http.ResponseWriter, r *http.Request)
	GetEscalations(w http.ResponseWriter, r *http.Request)
	GetDecisionQuality(w http.ResponseWriter, r *http.Request)
	GetCompliance(w http.ResponseWriter, r *http.Request)
	ExportReport(w http.ResponseWriter, r *http.Request)
}

type PolicyHandler interface {
	CreatePolicy(w http.ResponseWriter, r *http.Request)
	GetPolicy(w http.ResponseWriter, r *http.Request)
	ListPolicies(w http.ResponseWriter, r *http.Request)
	UpdatePolicy(w http.ResponseWriter, r *http.Request)
	DeletePolicy(w http.ResponseWriter, r *http.Request)
	TogglePolicyEnabled(w http.ResponseWriter, r *http.Request)
}

type AuthHandler interface {
	Register(w http.ResponseWriter, r *http.Request)
	Login(w http.ResponseWriter, r *http.Request)
	RefreshToken(w http.ResponseWriter, r *http.Request)
	GetProfile(w http.ResponseWriter, r *http.Request)
	UpdateProfile(w http.ResponseWriter, r *http.Request)
	ChangePassword(w http.ResponseWriter, r *http.Request)
}

type AdminHandler interface {
	ListUsers(w http.ResponseWriter, r *http.Request)
	GetUser(w http.ResponseWriter, r *http.Request)
	CreateUser(w http.ResponseWriter, r *http.Request)
	UpdateUser(w http.ResponseWriter, r *http.Request)
	DisableUser(w http.ResponseWriter, r *http.Request)
}

// VerificationHandler serves behavioral-verification endpoints (#20):
// attestation ingest (per-repo key auth), per-commit status, and key admin.
type VerificationHandler interface {
	Ingest(w http.ResponseWriter, r *http.Request)
	GetBySHA(w http.ResponseWriter, r *http.Request)
	CommitStatus(w http.ResponseWriter, r *http.Request)
	CreateKey(w http.ResponseWriter, r *http.Request)
	RevokeKey(w http.ResponseWriter, r *http.Request)
	ListKeys(w http.ResponseWriter, r *http.Request)
}

// Middleware is a function that wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// RouterConfig holds the configuration for the API router.
type RouterConfig struct {
	SessionHandler    SessionHandler
	GovernanceHandler GovernanceHandler
	AuditHandler      AuditHandler
	ServiceHandler    ServiceHandler
	TenantHandler     TenantHandler
	ReportsHandler    ReportsHandler
	PolicyHandler     PolicyHandler
	// DocStore is optional; when set, the read-only /v1/docs endpoints serve
	// documentation generated by the ADP documentation engine. Nil makes those
	// endpoints return 503.
	DocStore store.DocStore
	// CommitVerifier is optional; when set, POST /v1/commits/verify-batch reports
	// whether a set of commit SHAs all have a verified governance trail — the
	// basis for a server-side merge gate. Nil makes that endpoint return 503.
	CommitVerifier CommitVerifier
	// VerificationHandler is optional; when set, behavioral-verification
	// endpoints are served. Nil makes them unavailable.
	VerificationHandler VerificationHandler
	// Reconciler is optional; when set, the /v1/enforcement endpoints record and
	// list ungoverned-activity findings. Nil makes them return 503.
	Reconciler         *enforcement.Reconciler
	AuthMiddleware     Middleware
	RateLimiter        Middleware
	Validator          Middleware
	AuthHandler        AuthHandler
	AdminHandler       AdminHandler
	TenantMiddleware   Middleware
	CORSAllowedOrigins []string
	ReadinessCheck     func() map[string]string
}

// NewRouter creates and configures the API router with all handlers.
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	// Health check (no auth required)
	mux.HandleFunc("GET /health", healthCheck)
	if cfg.ReadinessCheck != nil {
		checker := cfg.ReadinessCheck
		mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
			checks := checker()
			allOk := true
			for _, v := range checks {
				if v != "ok" {
					allOk = false
					break
				}
			}
			status := http.StatusOK
			readyStatus := "ready"
			if !allOk {
				status = http.StatusServiceUnavailable
				readyStatus = "not_ready"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": readyStatus,
				"checks": checks,
			})
		})
	} else {
		mux.HandleFunc("GET /ready", readinessCheck)
	}

	// API version info
	mux.HandleFunc("GET /v1", apiInfo)

	// Session endpoints
	if cfg.SessionHandler != nil {
		mux.HandleFunc("POST /v1/sessions", cfg.SessionHandler.CreateSession)
		mux.HandleFunc("GET /v1/sessions", cfg.SessionHandler.ListSessions)
		mux.HandleFunc("GET /v1/sessions/{id}", cfg.SessionHandler.GetSession)
		mux.HandleFunc("PATCH /v1/sessions/{id}", cfg.SessionHandler.UpdateSession)
		mux.HandleFunc("DELETE /v1/sessions/{id}", cfg.SessionHandler.EndSession)
		mux.HandleFunc("PATCH /v1/sessions/{id}/heartbeat", cfg.SessionHandler.Heartbeat)
	}

	// Governance endpoints
	if cfg.GovernanceHandler != nil {
		mux.HandleFunc("POST /v1/governance/check", cfg.GovernanceHandler.CheckAction)
		mux.HandleFunc("POST /v1/governance/approvals", cfg.GovernanceHandler.RequestApproval)
		mux.HandleFunc("GET /v1/governance/approvals", cfg.GovernanceHandler.ListApprovals)
		mux.HandleFunc("GET /v1/governance/approvals/pending", cfg.GovernanceHandler.ListPendingApprovals)
		mux.HandleFunc("GET /v1/governance/approvals/{id}", cfg.GovernanceHandler.GetApproval)
		mux.HandleFunc("PATCH /v1/governance/approvals/{id}", cfg.GovernanceHandler.ResolveApproval)
	}

	// Audit endpoints
	if cfg.AuditHandler != nil {
		mux.HandleFunc("POST /v1/audit/decisions", cfg.AuditHandler.LogDecision)
		mux.HandleFunc("GET /v1/audit/decisions", cfg.AuditHandler.ListDecisions)
		mux.HandleFunc("GET /v1/audit/decisions/{id}", cfg.AuditHandler.GetDecision)
		mux.HandleFunc("GET /v1/audit/decisions/{id}/lineage", cfg.AuditHandler.GetLineage)
		mux.HandleFunc("POST /v1/commits/prepare", cfg.AuditHandler.PrepareCommit)
		mux.HandleFunc("POST /v1/commits/register", cfg.AuditHandler.RegisterCommit)
		mux.HandleFunc("POST /v1/commits/verify", cfg.AuditHandler.VerifyCommit)
	}

	// Service endpoints
	if cfg.ServiceHandler != nil {
		mux.HandleFunc("POST /v1/services", cfg.ServiceHandler.CreateService)
		mux.HandleFunc("GET /v1/services", cfg.ServiceHandler.ListServices)
		mux.HandleFunc("GET /v1/services/{id}", cfg.ServiceHandler.GetService)
		mux.HandleFunc("PATCH /v1/services/{id}", cfg.ServiceHandler.UpdateService)
		mux.HandleFunc("DELETE /v1/services/{id}", cfg.ServiceHandler.DeleteService)
	}

	// Tenant endpoints (Phase 5 - Enterprise Features)
	if cfg.TenantHandler != nil {
		// Tenant management
		mux.HandleFunc("POST /v1/tenants", cfg.TenantHandler.CreateTenant)
		mux.HandleFunc("GET /v1/tenants", cfg.TenantHandler.ListTenants)
		mux.HandleFunc("GET /v1/tenants/{id}", cfg.TenantHandler.GetTenant)
		mux.HandleFunc("PATCH /v1/tenants/{id}", cfg.TenantHandler.UpdateTenant)
		// Organization management
		mux.HandleFunc("POST /v1/tenants/{tenant_id}/organizations", cfg.TenantHandler.CreateOrganization)
		mux.HandleFunc("GET /v1/tenants/{tenant_id}/organizations", cfg.TenantHandler.ListOrganizations)
		mux.HandleFunc("GET /v1/organizations/{id}", cfg.TenantHandler.GetOrganization)
		// Team management
		mux.HandleFunc("POST /v1/organizations/{org_id}/teams", cfg.TenantHandler.CreateTeam)
		mux.HandleFunc("GET /v1/organizations/{org_id}/teams", cfg.TenantHandler.ListTeams)
		mux.HandleFunc("GET /v1/teams/{id}", cfg.TenantHandler.GetTeam)
		mux.HandleFunc("POST /v1/teams/{id}/members", cfg.TenantHandler.AddTeamMember)
		mux.HandleFunc("DELETE /v1/teams/{id}/members/{user_id}", cfg.TenantHandler.RemoveTeamMember)
		mux.HandleFunc("GET /v1/users/{user_id}/teams", cfg.TenantHandler.GetUserTeams)
	}

	// Reports endpoints (Phase 3.4 + Phase 4)
	if cfg.ReportsHandler != nil {
		mux.HandleFunc("GET /v1/reports/summary", cfg.ReportsHandler.GetSummary)
		mux.HandleFunc("GET /v1/reports/adoption", cfg.ReportsHandler.GetAdoption)
		mux.HandleFunc("GET /v1/reports/governance", cfg.ReportsHandler.GetGovernance)
		mux.HandleFunc("GET /v1/reports/escalations", cfg.ReportsHandler.GetEscalations)
		mux.HandleFunc("GET /v1/reports/decisions", cfg.ReportsHandler.GetDecisionQuality)
		mux.HandleFunc("GET /v1/reports/compliance", cfg.ReportsHandler.GetCompliance)
		mux.HandleFunc("POST /v1/reports/export", cfg.ReportsHandler.ExportReport)
	}

	// Policy endpoints
	if cfg.PolicyHandler != nil {
		mux.HandleFunc("POST /v1/policies", cfg.PolicyHandler.CreatePolicy)
		mux.HandleFunc("GET /v1/policies", cfg.PolicyHandler.ListPolicies)
		mux.HandleFunc("GET /v1/policies/{id}", cfg.PolicyHandler.GetPolicy)
		mux.HandleFunc("PATCH /v1/policies/{id}", cfg.PolicyHandler.UpdatePolicy)
		mux.HandleFunc("DELETE /v1/policies/{id}", cfg.PolicyHandler.DeletePolicy)
		mux.HandleFunc("PATCH /v1/policies/{id}/toggle", cfg.PolicyHandler.TogglePolicyEnabled)
	}

	// Auth endpoints (public — no auth required for register/login/refresh)
	if cfg.AuthHandler != nil {
		mux.HandleFunc("POST /v1/auth/register", cfg.AuthHandler.Register)
		mux.HandleFunc("POST /v1/auth/login", cfg.AuthHandler.Login)
		mux.HandleFunc("POST /v1/auth/refresh", cfg.AuthHandler.RefreshToken)
		mux.HandleFunc("GET /v1/auth/me", cfg.AuthHandler.GetProfile)
		mux.HandleFunc("PATCH /v1/auth/me", cfg.AuthHandler.UpdateProfile)
		mux.HandleFunc("POST /v1/auth/change-password", cfg.AuthHandler.ChangePassword)
	}

	// Admin endpoints (auth enforced in handler)
	if cfg.AdminHandler != nil {
		mux.HandleFunc("POST /v1/admin/users", cfg.AdminHandler.CreateUser)
		mux.HandleFunc("GET /v1/admin/users", cfg.AdminHandler.ListUsers)
		mux.HandleFunc("GET /v1/admin/users/{id}", cfg.AdminHandler.GetUser)
		mux.HandleFunc("PATCH /v1/admin/users/{id}", cfg.AdminHandler.UpdateUser)
		mux.HandleFunc("DELETE /v1/admin/users/{id}", cfg.AdminHandler.DisableUser)
	}

	// Context endpoint
	mux.HandleFunc("POST /v1/context", contextHandler)

	// Documentation (read-only; generated by the ADP documentation engine)
	mux.HandleFunc("GET /v1/docs", docsListHandler(cfg.DocStore))
	mux.HandleFunc("GET /v1/docs/{id}", docGetHandler(cfg.DocStore))

	// Server-side merge gate: authoritative batch verification of commit SHAs
	mux.HandleFunc("POST /v1/commits/verify-batch", verifyBatchHandler(cfg.CommitVerifier))

	// Behavioral verification: attestation ingest + per-commit status + keys.
	// POST /v1/verifications authenticates with a per-repo verification key
	// (see authMiddlewareWrapper) — evidence must come from CI, not the agent.
	if cfg.VerificationHandler != nil {
		mux.HandleFunc("POST /v1/verifications", cfg.VerificationHandler.Ingest)
		mux.HandleFunc("GET /v1/verifications/{sha}", cfg.VerificationHandler.GetBySHA)
		mux.HandleFunc("GET /v1/commits/{sha}/verification-status", cfg.VerificationHandler.CommitStatus)
		mux.HandleFunc("POST /v1/verification-keys", cfg.VerificationHandler.CreateKey)
		mux.HandleFunc("GET /v1/verification-keys", cfg.VerificationHandler.ListKeys)
		mux.HandleFunc("DELETE /v1/verification-keys/{id}", cfg.VerificationHandler.RevokeKey)
	}

	// Reconciliation: detect and surface ungoverned activity
	mux.HandleFunc("POST /v1/enforcement/commits/observed", observeCommitsHandler(cfg.Reconciler))
	mux.HandleFunc("GET /v1/enforcement/findings", listFindingsHandler(cfg.Reconciler))
	mux.HandleFunc("PATCH /v1/enforcement/findings/{id}", resolveFindingHandler(cfg.Reconciler))

	// Apply middleware chain
	var handler http.Handler = mux

	// Request validation
	if cfg.Validator != nil {
		handler = cfg.Validator(handler)
	}

	// Rate limiting
	if cfg.RateLimiter != nil {
		handler = cfg.RateLimiter(handler)
	}

	// Authentication (skip for health checks)
	if cfg.AuthMiddleware != nil {
		handler = authMiddlewareWrapper(cfg.AuthMiddleware, handler)
	}

	// CORS and common headers
	handler = corsMiddleware(cfg.CORSAllowedOrigins)(handler)
	handler = securityHeaders(handler)
	handler = loggingMiddleware(handler)

	return handler
}

// NewSimpleRouter creates a simple router for basic usage.
func NewSimpleRouter() http.Handler {
	return NewRouter(RouterConfig{})
}

// Health check handlers

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func readinessCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ready",
		"checks": map[string]string{
			"database": "ok",
			"cache":    "ok",
		},
	})
}

func apiInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "ADP API",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"sessions":   "/v1/sessions",
			"governance": "/v1/governance",
			"audit":      "/v1/audit",
			"services":   "/v1/services",
			"context":    "/v1/context",
		},
	})
}

// Context handler placeholder
// estimateTokens returns a rough token estimate for a piece of text using the
// common ~4-characters-per-token heuristic.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

// contextHandler implements POST /v1/context.
//
// This is a lightweight, infrastructure-free context assembler: it validates the
// request and returns a token-budgeted, request-scoped context envelope in the
// shape clients expect (layers.{essential,task_relevant,supporting} with
// content/tokens/sources). It does NOT perform vector/semantic retrieval — that
// requires the full context.Engine (vector store + embeddings + service
// registry), which is not wired into adp-server. The response sets
// "degraded": true so callers can tell real retrieval is not enabled.
func contextHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID   string `json:"session_id"`
		ServiceID   string `json:"service_id"`
		Task        string `json:"task"`
		TokenBudget struct {
			Essential    int `json:"essential"`
			TaskRelevant int `json:"task_relevant"`
			Supporting   int `json:"supporting"`
		} `json:"token_budget"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	if req.Task == "" {
		http.Error(w, "task is required", http.StatusBadRequest)
		return
	}

	// Default token budgets (per layer).
	if req.TokenBudget.Essential == 0 {
		req.TokenBudget.Essential = 4000
	}
	if req.TokenBudget.TaskRelevant == 0 {
		req.TokenBudget.TaskRelevant = 12000
	}
	if req.TokenBudget.Supporting == 0 {
		req.TokenBudget.Supporting = 8000
	}

	start := time.Now()

	serviceScope := req.ServiceID
	if serviceScope == "" {
		serviceScope = "(unscoped)"
	}

	essential := "# Governance context\n" +
		"Session: " + req.SessionID + "\n" +
		"Service scope: " + serviceScope + "\n" +
		"All actions must be authorized via a governance check before execution; " +
		"sensitive actions may require human approval."
	taskRelevant := "# Task\n" + req.Task + "\n\nTarget service: " + serviceScope
	supporting := "# Supporting\n" +
		"Semantic retrieval (codebase and documentation) is not enabled on this " +
		"endpoint; only request-scoped context is returned."

	// Report each layer's tokens, clamped to its budget.
	clamp := func(n, budget int) int {
		if n > budget {
			return budget
		}
		return n
	}
	essTokens := clamp(estimateTokens(essential), req.TokenBudget.Essential)
	taskTokens := clamp(estimateTokens(taskRelevant), req.TokenBudget.TaskRelevant)
	supTokens := clamp(estimateTokens(supporting), req.TokenBudget.Supporting)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": map[string]interface{}{
			"session_id": req.SessionID,
			"service_id": req.ServiceID,
			"task":       req.Task,
			"layers": map[string]interface{}{
				"essential": map[string]interface{}{
					"content": essential,
					"tokens":  essTokens,
					"sources": []string{"session:" + req.SessionID},
				},
				"task_relevant": map[string]interface{}{
					"content": taskRelevant,
					"tokens":  taskTokens,
					"sources": []string{"request"},
				},
				"supporting": map[string]interface{}{
					"content": supporting,
					"tokens":  supTokens,
					"sources": []string{},
				},
			},
			"total_tokens":      essTokens + taskTokens + supTokens,
			"cache_hit":         false,
			"retrieval_time_ms": time.Since(start).Milliseconds(),
			"degraded":          true,
			"note":              "Lightweight request-scoped context; vector retrieval not enabled. See internal/domain/context.Engine for full assembly.",
		},
	})
}

// docsListHandler implements GET /v1/docs. It lists documentation generated by
// the ADP documentation engine, filtered by session_id, free-text query, or
// category (default "session_summary"). Returns 503 when no doc store is wired.
func docsListHandler(docs store.DocStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if docs == nil {
			http.Error(w, "documentation store not configured", http.StatusServiceUnavailable)
			return
		}

		q := r.URL.Query()
		limit := 20
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		var (
			records []*store.DocRecord
			err     error
		)
		switch {
		case q.Get("session_id") != "":
			records, err = docs.ListBySession(r.Context(), q.Get("session_id"))
		case q.Get("query") != "":
			records, err = docs.Search(r.Context(), q.Get("query"), limit)
		default:
			category := q.Get("category")
			if category == "" {
				category = "session_summary"
			}
			records, err = docs.ListByCategory(r.Context(), category, limit)
		}
		if err != nil {
			http.Error(w, "failed to list documentation", http.StatusInternalServerError)
			return
		}
		if records == nil {
			records = []*store.DocRecord{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items":  records,
			"total":  len(records),
			"limit":  limit,
			"offset": 0,
		})
	}
}

// docGetHandler implements GET /v1/docs/{id}, returning a single generated
// document (Markdown content). Returns 503 when no doc store is wired.
func docGetHandler(docs store.DocStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if docs == nil {
			http.Error(w, "documentation store not configured", http.StatusServiceUnavailable)
			return
		}

		doc, err := docs.Get(r.Context(), r.PathValue("id"))
		if err != nil || doc == nil {
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": doc})
	}
}

// verifyBatchHandler implements POST /v1/commits/verify-batch. Given a list of
// commit SHAs it reports, server-side and authoritatively, whether every commit
// has a verified ADP governance trail. This backs a forge merge gate (required
// status check) that an agent cannot bypass — unlike client-side git hooks,
// which can be skipped with --no-verify. Always responds 200 on success with an
// `allowed` decision; the caller (CI job) fails the check when allowed is false.
// Returns 503 when no commit store is wired.
func verifyBatchHandler(commits CommitVerifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if commits == nil {
			http.Error(w, "commit store not configured", http.StatusServiceUnavailable)
			return
		}

		var req struct {
			SHAs []string `json:"shas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if len(req.SHAs) == 0 {
			http.Error(w, "shas is required", http.StatusBadRequest)
			return
		}

		type result struct {
			SHA      string `json:"sha"`
			Verified bool   `json:"verified"`
		}
		results := make([]result, 0, len(req.SHAs))
		unverified := []string{}
		verified := 0
		for _, sha := range req.SHAs {
			ok, err := commits.IsCommitVerified(r.Context(), sha)
			if err != nil {
				http.Error(w, "verification lookup failed", http.StatusInternalServerError)
				return
			}
			results = append(results, result{SHA: sha, Verified: ok})
			if ok {
				verified++
			} else {
				unverified = append(unverified, sha)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"allowed":    len(unverified) == 0,
			"total":      len(req.SHAs),
			"verified":   verified,
			"results":    results,
			"unverified": unverified,
		})
	}
}

// observeCommitsHandler implements POST /v1/enforcement/commits/observed. A
// push webhook posts observed commits; any without a governance trail are
// recorded as findings. Returns 503 when no reconciler is wired.
func observeCommitsHandler(rec *enforcement.Reconciler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rec == nil {
			http.Error(w, "reconciler not configured", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			Commits []enforcement.ObservedCommit `json:"commits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		findings, err := rec.ObserveCommits(r.Context(), req.Commits)
		if err != nil {
			http.Error(w, "reconciliation failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"observed": len(req.Commits),
			"flagged":  len(findings),
			"findings": findings,
		})
	}
}

// listFindingsHandler implements GET /v1/enforcement/findings, optionally
// filtered by ?status=open|acknowledged|resolved. Backs the Backstage console.
func listFindingsHandler(rec *enforcement.Reconciler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rec == nil {
			http.Error(w, "reconciler not configured", http.StatusServiceUnavailable)
			return
		}
		items := rec.Findings(enforcement.FindingStatus(r.URL.Query().Get("status")))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"items": items,
			"total": len(items),
		})
	}
}

// resolveFindingHandler implements PATCH /v1/enforcement/findings/{id}, setting
// a finding's status (acknowledge/resolve).
func resolveFindingHandler(rec *enforcement.Reconciler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rec == nil {
			http.Error(w, "reconciler not configured", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		status := enforcement.FindingStatus(req.Status)
		switch status {
		case enforcement.StatusOpen, enforcement.StatusAcknowledged, enforcement.StatusResolved:
		default:
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		f, ok := rec.Resolve(r.PathValue("id"), status)
		if !ok {
			http.Error(w, "finding not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": f})
	}
}

// Middleware helpers

func authMiddlewareWrapper(auth Middleware, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth entirely for health checks and stateless auth endpoints
		switch r.URL.Path {
		case "/health", "/ready",
			"/v1/auth/login", "/v1/auth/refresh":
			next.ServeHTTP(w, r)
			return
		}

		// Attestation ingest authenticates with a per-repo verification key
		// checked inside the handler — skip global auth for that path only.
		if r.URL.Path == "/v1/verifications" && r.Method == http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		// Register uses optional auth: try to authenticate (to detect admin callers)
		// but don't reject unauthenticated requests (first user needs to register).
		if r.URL.Path == "/v1/auth/register" {
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-Key") != "" {
				// Has credentials — run through auth middleware to inject context
				auth(next).ServeHTTP(w, r)
			} else {
				// No credentials — pass through without auth
				next.ServeHTTP(w, r)
			}
			return
		}

		auth(next).ServeHTTP(w, r)
	})
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if len(allowedOrigins) == 0 || (len(allowedOrigins) == 1 && allowedOrigins[0] == "*") {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" {
				for _, o := range allowedOrigins {
					if o == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						w.Header().Set("Vary", "Origin")
						break
					}
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors='none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		// Log request - in production use structured logging
		_ = struct {
			Method   string
			Path     string
			Status   int
			Duration time.Duration
		}{
			Method:   r.Method,
			Path:     r.URL.Path,
			Status:   rw.statusCode,
			Duration: time.Since(start),
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
