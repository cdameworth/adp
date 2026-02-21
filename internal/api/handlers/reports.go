package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/adp/adp/internal/infrastructure/database"
	"github.com/google/uuid"
)

// ReportHandler handles reporting API endpoints
type ReportHandler struct {
	store *database.ReportingStore
}

// NewReportHandler creates a new report handler
func NewReportHandler(store *database.ReportingStore) *ReportHandler {
	return &ReportHandler{store: store}
}

// parseReportFilter extracts common filter parameters from request
func parseReportFilter(r *http.Request) (database.ReportFilter, error) {
	filter := database.ReportFilter{
		Granularity: "day",
	}

	// Parse organization ID (required)
	orgIDStr := r.URL.Query().Get("organization_id")
	if orgIDStr == "" {
		// Try to get from context (would be set by auth middleware)
		// For now, use a default
		orgIDStr = r.Header.Get("X-Organization-ID")
	}
	if orgIDStr != "" {
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			return filter, fmt.Errorf("invalid organization_id: %w", err)
		}
		filter.OrganizationID = orgID
	}

	// Parse time range
	startStr := r.URL.Query().Get("start")
	if startStr != "" {
		start, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			return filter, fmt.Errorf("invalid start time: %w", err)
		}
		filter.Start = start
	} else {
		// Default to last 7 days
		filter.Start = time.Now().AddDate(0, 0, -7)
	}

	endStr := r.URL.Query().Get("end")
	if endStr != "" {
		end, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			return filter, fmt.Errorf("invalid end time: %w", err)
		}
		filter.End = end
	} else {
		filter.End = time.Now()
	}

	// Parse optional filters
	filter.AgentTool = r.URL.Query().Get("agent_tool")

	if serviceIDStr := r.URL.Query().Get("service_id"); serviceIDStr != "" {
		serviceID, err := uuid.Parse(serviceIDStr)
		if err == nil {
			filter.ServiceID = serviceID
		}
	}

	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		userID, err := uuid.Parse(userIDStr)
		if err == nil {
			filter.UserID = userID
		}
	}

	if granularity := r.URL.Query().Get("granularity"); granularity != "" {
		filter.Granularity = granularity
	}

	return filter, nil
}

// GetSummary handles GET /v1/reports/summary
func (h *ReportHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	report, err := h.store.GetSummaryReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to generate summary report: %v", err), "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// GetAdoption handles GET /v1/reports/adoption
func (h *ReportHandler) GetAdoption(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	metrics, err := h.store.GetAdoptionMetrics(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate adoption metrics", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"metrics":      metrics,
		"filter":       filter,
		"generated_at": time.Now(),
	})
}

// GetGovernance handles GET /v1/reports/governance
func (h *ReportHandler) GetGovernance(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	metrics, err := h.store.GetGovernanceMetrics(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate governance metrics", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

// GetEscalations handles GET /v1/reports/escalations
func (h *ReportHandler) GetEscalations(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	metrics, err := h.store.GetEscalationMetrics(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate escalation metrics", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, metrics)
}

// GetCompliance handles GET /v1/reports/compliance
func (h *ReportHandler) GetCompliance(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	report, err := h.store.GetComplianceReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate compliance report", "REPORT_FAILED")
		return
	}

	// Check if CSV export requested
	format := r.URL.Query().Get("format")
	if format == "csv" {
		h.exportComplianceCSV(w, report)
		return
	}

	writeJSON(w, http.StatusOK, report)
}

// exportComplianceCSV exports compliance report as CSV
func (h *ReportHandler) exportComplianceCSV(w http.ResponseWriter, report *database.ComplianceReport) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=compliance_report.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header
	writer.Write([]string{"Metric", "Value"})

	// Data
	writer.Write([]string{"Period", report.Period})
	writer.Write([]string{"Audit Trail Completeness", fmt.Sprintf("%.2f%%", report.AuditTrailCompleteness*100)})
	writer.Write([]string{"Unverified Attempts", strconv.FormatUint(report.UnverifiedAttempts, 10)})
	writer.Write([]string{"Sensitive Path Access", strconv.FormatUint(report.SensitivePathAccess, 10)})
	writer.Write([]string{"Off Hours Activity", strconv.FormatUint(report.OffHoursActivity, 10)})
	writer.Write([]string{"Production Activity", strconv.FormatUint(report.ProductionActivity, 10)})
	writer.Write([]string{"Decisions With Reasoning", strconv.FormatUint(report.DecisionsWithReasoning, 10)})
	writer.Write([]string{"Total Decisions", strconv.FormatUint(report.TotalDecisions, 10)})
}

// GetByService handles GET /v1/reports/by-service/{service_id}
func (h *ReportHandler) GetByService(w http.ResponseWriter, r *http.Request) {
	serviceIDStr := getPathParam(r, "service_id")
	serviceID, err := uuid.Parse(serviceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid service_id", "INVALID_ID")
		return
	}

	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}
	filter.ServiceID = serviceID

	// Get summary for this service
	summary, err := h.store.GetSummaryReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate service report", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service_id": serviceID,
		"summary":    summary,
		"filter":     filter,
	})
}

// GetByUser handles GET /v1/reports/by-user/{user_id}
func (h *ReportHandler) GetByUser(w http.ResponseWriter, r *http.Request) {
	userIDStr := getPathParam(r, "user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id", "INVALID_ID")
		return
	}

	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}
	filter.UserID = userID

	summary, err := h.store.GetSummaryReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate user report", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"summary": summary,
		"filter":  filter,
	})
}

// GetByAgent handles GET /v1/reports/by-agent/{agent_tool}
func (h *ReportHandler) GetByAgent(w http.ResponseWriter, r *http.Request) {
	agentTool := getPathParam(r, "agent_tool")
	if agentTool == "" {
		writeError(w, http.StatusBadRequest, "agent_tool is required", "MISSING_REQUIRED_FIELDS")
		return
	}

	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}
	filter.AgentTool = agentTool

	summary, err := h.store.GetSummaryReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate agent report", "REPORT_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agent_tool": agentTool,
		"summary":    summary,
		"filter":     filter,
	})
}

// GetByPolicy handles GET /v1/reports/by-policy/{policy_name}
func (h *ReportHandler) GetByPolicy(w http.ResponseWriter, r *http.Request) {
	policyName := getPathParam(r, "policy_name")
	if policyName == "" {
		writeError(w, http.StatusBadRequest, "policy_name is required", "MISSING_REQUIRED_FIELDS")
		return
	}

	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	// Get governance metrics (includes policy-level data)
	governance, err := h.store.GetGovernanceMetrics(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate policy report", "REPORT_FAILED")
		return
	}

	// Filter to specific policy
	var policyMetrics *database.PolicyDenialCount
	for _, p := range governance.TopDeniedPolicies {
		if p.PolicyName == policyName {
			policyMetrics = &p
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"policy_name":    policyName,
		"policy_metrics": policyMetrics,
		"overall":        governance,
		"filter":         filter,
	})
}

// ExportReport handles report export in various formats
func (h *ReportHandler) ExportReport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	// Gather all report data
	summary, _ := h.store.GetSummaryReport(r.Context(), filter)
	adoption, _ := h.store.GetAdoptionMetrics(r.Context(), filter)
	governance, _ := h.store.GetGovernanceMetrics(r.Context(), filter)
	escalations, _ := h.store.GetEscalationMetrics(r.Context(), filter)
	compliance, _ := h.store.GetComplianceReport(r.Context(), filter)

	report := map[string]interface{}{
		"generated_at": time.Now(),
		"filter":       filter,
		"summary":      summary,
		"adoption":     adoption,
		"governance":   governance,
		"escalations":  escalations,
		"compliance":   compliance,
	}

	switch format {
	case "csv":
		h.exportReportCSV(w, report)
	case "prometheus":
		h.exportPrometheusMetrics(w, summary, governance, escalations)
	default:
		writeJSON(w, http.StatusOK, report)
	}
}

func (h *ReportHandler) exportReportCSV(w http.ResponseWriter, report map[string]interface{}) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=adp_report.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"Section", "Metric", "Value"})

	if summary, ok := report["summary"].(*database.SummaryReport); ok && summary != nil {
		writer.Write([]string{"Summary", "Active Sessions", strconv.FormatUint(summary.ActiveSessions, 10)})
		writer.Write([]string{"Summary", "Total Sessions", strconv.FormatUint(summary.TotalSessions, 10)})
		writer.Write([]string{"Summary", "Total Decisions", strconv.FormatUint(summary.TotalDecisions, 10)})
		writer.Write([]string{"Summary", "Total Commits", strconv.FormatUint(summary.TotalCommits, 10)})
		writer.Write([]string{"Summary", "Policy Denials", strconv.FormatUint(summary.PolicyDenials, 10)})
		writer.Write([]string{"Summary", "Avg Confidence", fmt.Sprintf("%.2f", summary.AvgConfidence)})
	}

	if governance, ok := report["governance"].(*database.GovernanceMetrics); ok && governance != nil {
		writer.Write([]string{"Governance", "Policy Checks", strconv.FormatUint(governance.PolicyChecks, 10)})
		writer.Write([]string{"Governance", "Allow Rate", fmt.Sprintf("%.2f%%", governance.AllowRate*100)})
		writer.Write([]string{"Governance", "Deny Rate", fmt.Sprintf("%.2f%%", governance.DenyRate*100)})
	}
}

func (h *ReportHandler) exportPrometheusMetrics(w http.ResponseWriter, summary *database.SummaryReport, governance *database.GovernanceMetrics, escalations *database.EscalationMetrics) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	fmt.Fprintln(w, "# HELP adp_sessions_active Current active sessions")
	fmt.Fprintln(w, "# TYPE adp_sessions_active gauge")
	if summary != nil {
		fmt.Fprintf(w, "adp_sessions_active %d\n", summary.ActiveSessions)
	}

	fmt.Fprintln(w, "# HELP adp_sessions_total Total sessions in period")
	fmt.Fprintln(w, "# TYPE adp_sessions_total counter")
	if summary != nil {
		fmt.Fprintf(w, "adp_sessions_total %d\n", summary.TotalSessions)
	}

	fmt.Fprintln(w, "# HELP adp_decisions_total Total decisions logged")
	fmt.Fprintln(w, "# TYPE adp_decisions_total counter")
	if summary != nil {
		fmt.Fprintf(w, "adp_decisions_total %d\n", summary.TotalDecisions)
	}

	fmt.Fprintln(w, "# HELP adp_policy_checks_total Total policy evaluations")
	fmt.Fprintln(w, "# TYPE adp_policy_checks_total counter")
	if governance != nil {
		fmt.Fprintf(w, "adp_policy_checks_total %d\n", governance.PolicyChecks)
	}

	fmt.Fprintln(w, "# HELP adp_policy_denied_total Total policy denials")
	fmt.Fprintln(w, "# TYPE adp_policy_denied_total counter")
	if governance != nil {
		fmt.Fprintf(w, "adp_policy_denied_total %d\n", governance.Denied)
	}

	fmt.Fprintln(w, "# HELP adp_escalations_total Total escalation requests")
	fmt.Fprintln(w, "# TYPE adp_escalations_total counter")
	if escalations != nil {
		fmt.Fprintf(w, "adp_escalations_total %d\n", escalations.TotalEscalations)
	}

	fmt.Fprintln(w, "# HELP adp_escalations_approved Total approved escalations")
	fmt.Fprintln(w, "# TYPE adp_escalations_approved counter")
	if escalations != nil {
		fmt.Fprintf(w, "adp_escalations_approved %d\n", escalations.Approved)
	}

	fmt.Fprintln(w, "# HELP adp_confidence_avg Average decision confidence")
	fmt.Fprintln(w, "# TYPE adp_confidence_avg gauge")
	if summary != nil {
		fmt.Fprintf(w, "adp_confidence_avg %.4f\n", summary.AvgConfidence)
	}

	fmt.Fprintln(w, "# HELP adp_context_latency_ms Average context retrieval latency")
	fmt.Fprintln(w, "# TYPE adp_context_latency_ms gauge")
	if summary != nil {
		fmt.Fprintf(w, "adp_context_latency_ms %.2f\n", summary.AvgContextLatencyMs)
	}
}

// GetDecisionQuality handles GET /v1/reports/decisions
func (h *ReportHandler) GetDecisionQuality(w http.ResponseWriter, r *http.Request) {
	filter, err := parseReportFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "INVALID_REQUEST")
		return
	}

	// Get summary report which includes decision quality metrics
	report, err := h.store.GetSummaryReport(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate decision quality report", "REPORT_FAILED")
		return
	}

	// Return decision-focused metrics
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"period":               report.Period,
		"total_decisions":      report.TotalDecisions,
		"avg_confidence":       report.AvgConfidence,
		"low_confidence_count": report.LowConfidenceCount,
		"generated_at":         report.GeneratedAt,
	})
}

// Note: writeJSON is defined in handlers.go - removed duplicate
