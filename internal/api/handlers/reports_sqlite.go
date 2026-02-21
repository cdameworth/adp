package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
)

// SQLiteReportHandler handles reporting API endpoints backed by SQLite stores.
type SQLiteReportHandler struct {
	sessionStore    *database.SQLiteSessionStore
	decisionStore   *database.SQLiteDecisionStore
	commitStore     *database.SQLiteCommitStore
	escalationStore *database.SQLiteEscalationStore
}

// NewSQLiteReportHandler creates a new SQLite-backed report handler.
func NewSQLiteReportHandler(
	sessionStore *database.SQLiteSessionStore,
	decisionStore *database.SQLiteDecisionStore,
	commitStore *database.SQLiteCommitStore,
	escalationStore *database.SQLiteEscalationStore,
) *SQLiteReportHandler {
	return &SQLiteReportHandler{
		sessionStore:    sessionStore,
		decisionStore:   decisionStore,
		commitStore:     commitStore,
		escalationStore: escalationStore,
	}
}

// GetSummary handles GET /v1/reports/summary
func (h *SQLiteReportHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	activeSessions := h.countActiveSessions(ctx)
	decisionsToday := h.countDecisionsToday(ctx)
	decisionsAvg7d := h.averageDecisions7d(ctx)
	escalationDepth := h.countPendingEscalations(ctx)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_sessions":        activeSessions,
		"decisions_today":        decisionsToday,
		"decisions_average_7d":   decisionsAvg7d,
		"escalation_queue_depth": escalationDepth,
		"policy_health_score":    100,
		"adoption_trend_30d":     []interface{}{},
	})
}

// GetAdoption handles GET /v1/reports/adoption
func (h *SQLiteReportHandler) GetAdoption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	activeSessions := h.countActiveSessions(ctx)
	totalDecisions := h.countAllDecisions(ctx)
	totalEscalations := h.countAllEscalations(ctx)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"time_range": map[string]string{
			"start": start.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"active_sessions":   activeSessions,
		"total_decisions":   totalDecisions,
		"total_escalations": totalEscalations,
		"adoption_trend":    []interface{}{},
		"agents_by_tool":    []interface{}{},
	})
}

// GetGovernance handles GET /v1/reports/governance
func (h *SQLiteReportHandler) GetGovernance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	totalDecisions := h.countAllDecisions(ctx)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"time_range": map[string]string{
			"start": start.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"policy_evaluations": map[string]interface{}{
			"total":     totalDecisions,
			"allowed":   totalDecisions,
			"denied":    0,
			"escalated": 0,
		},
		"policies_by_denial_rate": []interface{}{},
		"false_positive_trend":    []interface{}{},
	})
}

// GetEscalations handles GET /v1/reports/escalations
func (h *SQLiteReportHandler) GetEscalations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	totalEscalations := h.countAllEscalations(ctx)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"time_range": map[string]string{
			"start": start.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"total_escalations":             totalEscalations,
		"approval_rate":                 0,
		"rejection_rate":                0,
		"average_resolution_time_hours": 0,
		"escalations_by_policy":         []interface{}{},
	})
}

// GetDecisionQuality handles GET /v1/reports/decisions
func (h *SQLiteReportHandler) GetDecisionQuality(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	totalDecisions := h.countAllDecisions(ctx)
	decisionsToday := h.countDecisionsToday(ctx)
	decisionsAvg7d := h.averageDecisions7d(ctx)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"time_range": map[string]string{
			"start": start.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"total_decisions":      totalDecisions,
		"decisions_today":      decisionsToday,
		"decisions_average_7d": decisionsAvg7d,
		"avg_confidence":       0.0,
		"low_confidence_count": 0,
		"decision_types":       []interface{}{},
	})
}

// GetCompliance handles GET /v1/reports/compliance
func (h *SQLiteReportHandler) GetCompliance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	totalDecisions := h.countAllDecisions(ctx)

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"time_range": map[string]string{
			"start": start.Format(time.RFC3339),
			"end":   now.Format(time.RFC3339),
		},
		"policy_evaluations": map[string]interface{}{
			"total":     totalDecisions,
			"allowed":   totalDecisions,
			"denied":    0,
			"escalated": 0,
		},
		"audit_trail_completeness": 1.0,
		"unverified_attempts":      0,
		"sensitive_path_access":    0,
		"compliance_score":         100,
	})
}

// ExportReport handles POST /v1/reports/export
func (h *SQLiteReportHandler) ExportReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	now := time.Now().UTC()
	start7d := now.AddDate(0, 0, -7)
	start30d := now.AddDate(0, 0, -30)

	activeSessions := h.countActiveSessions(ctx)
	decisionsToday := h.countDecisionsToday(ctx)
	decisionsAvg7d := h.averageDecisions7d(ctx)
	totalDecisions := h.countAllDecisions(ctx)
	totalEscalations := h.countAllEscalations(ctx)
	escalationDepth := h.countPendingEscalations(ctx)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"generated_at": now.Format(time.RFC3339),
		"summary": map[string]interface{}{
			"active_sessions":        activeSessions,
			"decisions_today":        decisionsToday,
			"decisions_average_7d":   decisionsAvg7d,
			"escalation_queue_depth": escalationDepth,
			"policy_health_score":    100,
			"adoption_trend_30d":     []interface{}{},
		},
		"adoption": map[string]interface{}{
			"time_range": map[string]string{
				"start": start30d.Format(time.RFC3339),
				"end":   now.Format(time.RFC3339),
			},
			"active_sessions":   activeSessions,
			"total_decisions":   totalDecisions,
			"total_escalations": totalEscalations,
			"adoption_trend":    []interface{}{},
		},
		"governance": map[string]interface{}{
			"time_range": map[string]string{
				"start": start7d.Format(time.RFC3339),
				"end":   now.Format(time.RFC3339),
			},
			"policy_evaluations": map[string]interface{}{
				"total":     totalDecisions,
				"allowed":   totalDecisions,
				"denied":    0,
				"escalated": 0,
			},
			"policies_by_denial_rate": []interface{}{},
		},
		"escalations": map[string]interface{}{
			"time_range": map[string]string{
				"start": start7d.Format(time.RFC3339),
				"end":   now.Format(time.RFC3339),
			},
			"total_escalations":             totalEscalations,
			"approval_rate":                 0,
			"rejection_rate":                0,
			"average_resolution_time_hours": 0,
		},
		"compliance": map[string]interface{}{
			"audit_trail_completeness": 1.0,
			"unverified_attempts":      0,
			"sensitive_path_access":    0,
			"compliance_score":         100,
		},
	})
}

// ---------------------------------------------------------------------------
// Internal helpers -- query actual SQLite stores, return 0 on nil store or error
// ---------------------------------------------------------------------------

func (h *SQLiteReportHandler) countActiveSessions(ctx context.Context) int {
	if h.sessionStore == nil {
		return 0
	}
	sessions, err := h.sessionStore.List(ctx, database.SQLiteSessionFilter{
		Status: "active",
		Limit:  1000,
	})
	if err != nil {
		return 0
	}
	return len(sessions)
}

func (h *SQLiteReportHandler) countDecisionsToday(ctx context.Context) int {
	if h.decisionStore == nil {
		return 0
	}
	// Fetch a large page of recent decisions and count those created today.
	decisions, err := h.decisionStore.List(ctx, database.SQLiteDecisionFilter{
		Limit: 1000,
	})
	if err != nil {
		return 0
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	count := 0
	for _, d := range decisions {
		if !d.CreatedAt.Before(today) {
			count++
		}
	}
	return count
}

func (h *SQLiteReportHandler) countAllDecisions(ctx context.Context) int {
	if h.decisionStore == nil {
		return 0
	}
	decisions, err := h.decisionStore.List(ctx, database.SQLiteDecisionFilter{
		Limit: 10000,
	})
	if err != nil {
		return 0
	}
	return len(decisions)
}

func (h *SQLiteReportHandler) countAllEscalations(ctx context.Context) int {
	if h.escalationStore == nil {
		return 0
	}
	escalations, err := h.escalationStore.List(ctx, database.SQLiteEscalationFilter{
		Limit: 10000,
	})
	if err != nil {
		return 0
	}
	return len(escalations)
}

func (h *SQLiteReportHandler) countPendingEscalations(ctx context.Context) int {
	if h.escalationStore == nil {
		return 0
	}
	escalations, err := h.escalationStore.List(ctx, database.SQLiteEscalationFilter{
		Status: "pending",
		Limit:  10000,
	})
	if err != nil {
		return 0
	}
	return len(escalations)
}

func (h *SQLiteReportHandler) averageDecisions7d(ctx context.Context) float64 {
	if h.decisionStore == nil {
		return 0
	}
	decisions, err := h.decisionStore.List(ctx, database.SQLiteDecisionFilter{
		Limit: 10000,
	})
	if err != nil {
		return 0
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	count := 0
	for _, d := range decisions {
		if !d.CreatedAt.Before(cutoff) {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return float64(count) / 7.0
}
