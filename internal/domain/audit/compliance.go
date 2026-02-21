// Package audit provides compliance reporting for regulatory requirements.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// ComplianceFramework represents a supported compliance framework
type ComplianceFramework string

const (
	FrameworkSOC2     ComplianceFramework = "soc2"
	FrameworkISO27001 ComplianceFramework = "iso27001"
	FrameworkGDPR     ComplianceFramework = "gdpr"
	FrameworkHIPAA    ComplianceFramework = "hipaa"
	FrameworkPCIDSS   ComplianceFramework = "pci-dss"
	FrameworkNIST     ComplianceFramework = "nist"
	FrameworkCustom   ComplianceFramework = "custom"
)

// ComplianceReport represents a generated compliance report
type ComplianceReport struct {
	ID             string              `json:"id"`
	Framework      ComplianceFramework `json:"framework"`
	TenantID       uuid.UUID           `json:"tenant_id"`
	OrganizationID *uuid.UUID          `json:"organization_id,omitempty"`
	GeneratedAt    time.Time           `json:"generated_at"`
	PeriodStart    time.Time           `json:"period_start"`
	PeriodEnd      time.Time           `json:"period_end"`
	GeneratedBy    string              `json:"generated_by"`

	// Summary metrics
	Summary ComplianceSummary `json:"summary"`

	// Control assessments
	Controls []ControlAssessment `json:"controls"`

	// Evidence
	Evidence []EvidenceItem `json:"evidence"`

	// Recommendations
	Recommendations []Recommendation `json:"recommendations"`

	// Metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ComplianceSummary provides high-level compliance metrics
type ComplianceSummary struct {
	TotalControls   int     `json:"total_controls"`
	PassingControls int     `json:"passing_controls"`
	FailingControls int     `json:"failing_controls"`
	NotApplicable   int     `json:"not_applicable"`
	ComplianceScore float64 `json:"compliance_score"` // Percentage
	RiskLevel       string  `json:"risk_level"`       // low, medium, high, critical

	// Key metrics
	AuditTrailCompleteness float64 `json:"audit_trail_completeness"`
	PolicyEnforcement      float64 `json:"policy_enforcement_rate"`
	EscalationResponseTime float64 `json:"escalation_response_time_hours"`
	SensitiveAccessEvents  int     `json:"sensitive_access_events"`
	UnauthorizedAttempts   int     `json:"unauthorized_attempts"`
	OffHoursActivity       int     `json:"off_hours_activity_count"`
}

// ControlAssessment represents the assessment of a specific control
type ControlAssessment struct {
	ControlID    string        `json:"control_id"`
	ControlName  string        `json:"control_name"`
	Category     string        `json:"category"`
	Description  string        `json:"description"`
	Status       ControlStatus `json:"status"`
	Findings     []Finding     `json:"findings,omitempty"`
	EvidenceRefs []string      `json:"evidence_refs,omitempty"`
	LastAssessed time.Time     `json:"last_assessed"`
	NextReview   *time.Time    `json:"next_review,omitempty"`
}

// ControlStatus represents the status of a control
type ControlStatus string

const (
	ControlPassing       ControlStatus = "passing"
	ControlFailing       ControlStatus = "failing"
	ControlPartial       ControlStatus = "partial"
	ControlNotApplicable ControlStatus = "not_applicable"
	ControlNotAssessed   ControlStatus = "not_assessed"
)

// Finding represents a compliance finding
type Finding struct {
	ID          string     `json:"id"`
	Severity    string     `json:"severity"` // low, medium, high, critical
	Description string     `json:"description"`
	Impact      string     `json:"impact"`
	Remediation string     `json:"remediation"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Status      string     `json:"status"` // open, in_progress, resolved, accepted
}

// EvidenceItem represents a piece of compliance evidence
type EvidenceItem struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"` // log, config, screenshot, document
	Description  string    `json:"description"`
	CollectedAt  time.Time `json:"collected_at"`
	ControlRefs  []string  `json:"control_refs"`
	DataLocation string    `json:"data_location,omitempty"`
	Hash         string    `json:"hash,omitempty"` // SHA256 for integrity
}

// Recommendation represents a compliance improvement recommendation
type Recommendation struct {
	ID          string   `json:"id"`
	Priority    string   `json:"priority"` // low, medium, high, critical
	Category    string   `json:"category"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Impact      string   `json:"impact"`
	Effort      string   `json:"effort"` // low, medium, high
	ControlRefs []string `json:"control_refs,omitempty"`
}

// ComplianceReporter generates compliance reports
type ComplianceReporter struct {
	eventStore   AuditEventStore
	metricsStore MetricsStore
	controlDefs  map[ComplianceFramework][]ControlDefinition
}

// MetricsStore interface for fetching compliance metrics
type MetricsStore interface {
	GetAuditTrailCompleteness(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (float64, error)
	GetPolicyEnforcementRate(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (float64, error)
	GetAvgEscalationResponseTime(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (float64, error)
	GetSensitiveAccessCount(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (int, error)
	GetUnauthorizedAttempts(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (int, error)
	GetOffHoursActivityCount(ctx context.Context, tenantID uuid.UUID, start, end time.Time) (int, error)
}

// ControlDefinition defines a compliance control
type ControlDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Framework   string `json:"framework"`
	Assessor    func(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error)
}

// NewComplianceReporter creates a new compliance reporter
func NewComplianceReporter(eventStore AuditEventStore, metricsStore MetricsStore) *ComplianceReporter {
	r := &ComplianceReporter{
		eventStore:   eventStore,
		metricsStore: metricsStore,
		controlDefs:  make(map[ComplianceFramework][]ControlDefinition),
	}

	// Register default control definitions
	r.registerSOC2Controls()
	r.registerISO27001Controls()

	return r
}

// GenerateReport generates a compliance report for the specified framework
func (r *ComplianceReporter) GenerateReport(ctx context.Context, req ReportRequest) (*ComplianceReport, error) {
	report := &ComplianceReport{
		ID:              uuid.New().String(),
		Framework:       req.Framework,
		TenantID:        req.TenantID,
		OrganizationID:  req.OrganizationID,
		GeneratedAt:     time.Now(),
		PeriodStart:     req.PeriodStart,
		PeriodEnd:       req.PeriodEnd,
		GeneratedBy:     req.GeneratedBy,
		Controls:        []ControlAssessment{},
		Evidence:        []EvidenceItem{},
		Recommendations: []Recommendation{},
		Metadata:        make(map[string]interface{}),
	}

	// Get control definitions for framework
	controls, ok := r.controlDefs[req.Framework]
	if !ok {
		return nil, fmt.Errorf("unsupported compliance framework: %s", req.Framework)
	}

	// Assess each control
	passingCount := 0
	failingCount := 0
	naCount := 0

	for _, controlDef := range controls {
		status, findings, err := controlDef.Assessor(ctx, r, req.TenantID, req.PeriodStart, req.PeriodEnd)
		if err != nil {
			// Log error but continue with other controls
			status = ControlNotAssessed
		}

		assessment := ControlAssessment{
			ControlID:    controlDef.ID,
			ControlName:  controlDef.Name,
			Category:     controlDef.Category,
			Description:  controlDef.Description,
			Status:       status,
			Findings:     findings,
			LastAssessed: time.Now(),
		}

		report.Controls = append(report.Controls, assessment)

		switch status {
		case ControlPassing:
			passingCount++
		case ControlFailing:
			failingCount++
		case ControlNotApplicable:
			naCount++
		}
	}

	// Calculate summary metrics
	totalControls := len(controls)
	assessedControls := totalControls - naCount

	var complianceScore float64
	if assessedControls > 0 {
		complianceScore = float64(passingCount) / float64(assessedControls) * 100
	}

	// Fetch additional metrics
	auditCompleteness, _ := r.metricsStore.GetAuditTrailCompleteness(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)
	policyRate, _ := r.metricsStore.GetPolicyEnforcementRate(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)
	escalationTime, _ := r.metricsStore.GetAvgEscalationResponseTime(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)
	sensitiveAccess, _ := r.metricsStore.GetSensitiveAccessCount(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)
	unauthorizedAttempts, _ := r.metricsStore.GetUnauthorizedAttempts(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)
	offHoursCount, _ := r.metricsStore.GetOffHoursActivityCount(ctx, req.TenantID, req.PeriodStart, req.PeriodEnd)

	report.Summary = ComplianceSummary{
		TotalControls:          totalControls,
		PassingControls:        passingCount,
		FailingControls:        failingCount,
		NotApplicable:          naCount,
		ComplianceScore:        complianceScore,
		RiskLevel:              r.calculateRiskLevel(complianceScore, failingCount),
		AuditTrailCompleteness: auditCompleteness,
		PolicyEnforcement:      policyRate,
		EscalationResponseTime: escalationTime,
		SensitiveAccessEvents:  sensitiveAccess,
		UnauthorizedAttempts:   unauthorizedAttempts,
		OffHoursActivity:       offHoursCount,
	}

	// Generate recommendations
	report.Recommendations = r.generateRecommendations(report)

	return report, nil
}

// ReportRequest specifies parameters for generating a compliance report
type ReportRequest struct {
	Framework       ComplianceFramework
	TenantID        uuid.UUID
	OrganizationID  *uuid.UUID
	PeriodStart     time.Time
	PeriodEnd       time.Time
	GeneratedBy     string
	IncludeEvidence bool
}

// calculateRiskLevel determines risk level based on compliance score
func (r *ComplianceReporter) calculateRiskLevel(score float64, failingCount int) string {
	if failingCount > 5 || score < 50 {
		return "critical"
	}
	if failingCount > 2 || score < 70 {
		return "high"
	}
	if failingCount > 0 || score < 90 {
		return "medium"
	}
	return "low"
}

// generateRecommendations generates recommendations based on report findings
func (r *ComplianceReporter) generateRecommendations(report *ComplianceReport) []Recommendation {
	var recommendations []Recommendation

	// Check for audit trail issues
	if report.Summary.AuditTrailCompleteness < 99.9 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Priority:    "high",
			Category:    "audit",
			Title:       "Improve Audit Trail Completeness",
			Description: fmt.Sprintf("Current audit trail completeness is %.2f%%. All agent decisions and actions must be logged.", report.Summary.AuditTrailCompleteness),
			Impact:      "Incomplete audit trails may result in compliance failures and inability to trace issues.",
			Effort:      "medium",
		})
	}

	// Check for escalation response time
	if report.Summary.EscalationResponseTime > 4 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Priority:    "medium",
			Category:    "process",
			Title:       "Reduce Escalation Response Time",
			Description: fmt.Sprintf("Average escalation response time is %.2f hours. Target is under 4 hours.", report.Summary.EscalationResponseTime),
			Impact:      "Long response times block agent workflows and reduce productivity.",
			Effort:      "low",
		})
	}

	// Check for unauthorized attempts
	if report.Summary.UnauthorizedAttempts > 0 {
		recommendations = append(recommendations, Recommendation{
			ID:          uuid.New().String(),
			Priority:    "high",
			Category:    "security",
			Title:       "Review Unauthorized Access Attempts",
			Description: fmt.Sprintf("%d unauthorized access attempts detected during the period.", report.Summary.UnauthorizedAttempts),
			Impact:      "Unauthorized attempts may indicate misconfigured agents or security threats.",
			Effort:      "medium",
		})
	}

	// Add recommendations for failing controls
	for _, control := range report.Controls {
		if control.Status == ControlFailing {
			recommendations = append(recommendations, Recommendation{
				ID:          uuid.New().String(),
				Priority:    "high",
				Category:    control.Category,
				Title:       fmt.Sprintf("Address Failing Control: %s", control.ControlName),
				Description: control.Description,
				Impact:      "Failing controls directly impact compliance posture.",
				Effort:      "medium",
				ControlRefs: []string{control.ControlID},
			})
		}
	}

	return recommendations
}

// registerSOC2Controls registers SOC 2 Type II control definitions
func (r *ComplianceReporter) registerSOC2Controls() {
	r.controlDefs[FrameworkSOC2] = []ControlDefinition{
		{
			ID:          "CC6.1",
			Name:        "Logical Access Security",
			Category:    "Access Control",
			Description: "The entity implements logical access security software, infrastructure, and architectures over protected information assets.",
			Assessor:    assessLogicalAccessControl,
		},
		{
			ID:          "CC6.2",
			Name:        "User Authentication",
			Category:    "Access Control",
			Description: "Prior to issuing system credentials and granting system access, the entity registers and authorizes new internal and external users.",
			Assessor:    assessUserAuthentication,
		},
		{
			ID:          "CC6.3",
			Name:        "Authorization and Modification",
			Category:    "Access Control",
			Description: "The entity authorizes, modifies, or removes access to data, software, functions, and other protected information assets.",
			Assessor:    assessAuthorizationControl,
		},
		{
			ID:          "CC7.1",
			Name:        "System Operations Monitoring",
			Category:    "System Operations",
			Description: "To meet its objectives, the entity uses detection and monitoring procedures to identify anomalies.",
			Assessor:    assessSystemMonitoring,
		},
		{
			ID:          "CC7.2",
			Name:        "Security Event Response",
			Category:    "System Operations",
			Description: "The entity evaluates events to determine whether they represent failures in achieving objectives.",
			Assessor:    assessEventResponse,
		},
		{
			ID:          "CC8.1",
			Name:        "Change Management",
			Category:    "Change Management",
			Description: "The entity authorizes, designs, develops, configures, documents, tests, approves, and implements changes.",
			Assessor:    assessChangeManagement,
		},
	}
}

// registerISO27001Controls registers ISO 27001 control definitions
func (r *ComplianceReporter) registerISO27001Controls() {
	r.controlDefs[FrameworkISO27001] = []ControlDefinition{
		{
			ID:          "A.9.1.1",
			Name:        "Access Control Policy",
			Category:    "Access Control",
			Description: "An access control policy shall be established, documented and reviewed based on business and information security requirements.",
			Assessor:    assessAccessControlPolicy,
		},
		{
			ID:          "A.9.2.1",
			Name:        "User Registration and De-registration",
			Category:    "Access Control",
			Description: "A formal user registration and de-registration process shall be implemented.",
			Assessor:    assessUserRegistration,
		},
		{
			ID:          "A.12.4.1",
			Name:        "Event Logging",
			Category:    "Operations Security",
			Description: "Event logs recording user activities, exceptions, faults and information security events shall be produced, kept and regularly reviewed.",
			Assessor:    assessEventLogging,
		},
		{
			ID:          "A.12.4.3",
			Name:        "Administrator and Operator Logs",
			Category:    "Operations Security",
			Description: "System administrator and system operator activities shall be logged and the logs protected and regularly reviewed.",
			Assessor:    assessAdminLogging,
		},
	}
}

// Control assessor functions

func assessLogicalAccessControl(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	// Check that all agent sessions have proper authentication
	policyRate, err := r.metricsStore.GetPolicyEnforcementRate(ctx, tenantID, start, end)
	if err != nil {
		return ControlNotAssessed, nil, err
	}

	var findings []Finding
	if policyRate < 100 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "medium",
			Description: fmt.Sprintf("Policy enforcement rate is %.2f%%, not 100%%", policyRate),
			Impact:      "Some agent actions may bypass policy checks",
			Remediation: "Review and fix any gaps in policy enforcement",
			Status:      "open",
		})
	}

	if policyRate >= 99.9 {
		return ControlPassing, findings, nil
	}
	if policyRate >= 95 {
		return ControlPartial, findings, nil
	}
	return ControlFailing, findings, nil
}

func assessUserAuthentication(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	unauthorizedAttempts, err := r.metricsStore.GetUnauthorizedAttempts(ctx, tenantID, start, end)
	if err != nil {
		return ControlNotAssessed, nil, err
	}

	var findings []Finding
	if unauthorizedAttempts > 0 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "high",
			Description: fmt.Sprintf("%d unauthorized access attempts detected", unauthorizedAttempts),
			Impact:      "Potential security breach or misconfiguration",
			Remediation: "Review and investigate all unauthorized attempts",
			Status:      "open",
		})
		return ControlFailing, findings, nil
	}

	return ControlPassing, findings, nil
}

func assessAuthorizationControl(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	// Assess that escalations are being handled properly
	escalationTime, err := r.metricsStore.GetAvgEscalationResponseTime(ctx, tenantID, start, end)
	if err != nil {
		return ControlNotAssessed, nil, err
	}

	var findings []Finding
	if escalationTime > 24 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "medium",
			Description: fmt.Sprintf("Average escalation response time is %.2f hours", escalationTime),
			Impact:      "Delayed authorization decisions",
			Remediation: "Improve escalation workflow and staffing",
			Status:      "open",
		})
		return ControlFailing, findings, nil
	}
	if escalationTime > 4 {
		return ControlPartial, findings, nil
	}

	return ControlPassing, findings, nil
}

func assessSystemMonitoring(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	auditCompleteness, err := r.metricsStore.GetAuditTrailCompleteness(ctx, tenantID, start, end)
	if err != nil {
		return ControlNotAssessed, nil, err
	}

	var findings []Finding
	if auditCompleteness < 99.9 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "high",
			Description: fmt.Sprintf("Audit trail completeness is %.2f%%", auditCompleteness),
			Impact:      "Gaps in monitoring and detection capability",
			Remediation: "Ensure all agent activities are logged",
			Status:      "open",
		})
		return ControlFailing, findings, nil
	}

	return ControlPassing, findings, nil
}

func assessEventResponse(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	// Check response to security events via escalation metrics
	escalationTime, _ := r.metricsStore.GetAvgEscalationResponseTime(ctx, tenantID, start, end)
	sensitiveAccess, _ := r.metricsStore.GetSensitiveAccessCount(ctx, tenantID, start, end)

	var findings []Finding

	if escalationTime > 4 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "medium",
			Description: "Security event response time exceeds 4 hours",
			Impact:      "Delayed response to security events",
			Remediation: "Improve incident response procedures",
			Status:      "open",
		})
	}

	if sensitiveAccess > 100 && escalationTime > 1 {
		return ControlFailing, findings, nil
	}
	if len(findings) > 0 {
		return ControlPartial, findings, nil
	}

	return ControlPassing, findings, nil
}

func assessChangeManagement(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	// Assess that all code changes go through proper approval
	policyRate, err := r.metricsStore.GetPolicyEnforcementRate(ctx, tenantID, start, end)
	if err != nil {
		return ControlNotAssessed, nil, err
	}

	var findings []Finding
	if policyRate < 100 {
		findings = append(findings, Finding{
			ID:          uuid.New().String(),
			Severity:    "medium",
			Description: "Not all changes are going through policy evaluation",
			Impact:      "Potential for unauthorized changes",
			Remediation: "Ensure all agent commits are verified",
			Status:      "open",
		})
		return ControlPartial, findings, nil
	}

	return ControlPassing, findings, nil
}

func assessAccessControlPolicy(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	return assessLogicalAccessControl(ctx, r, tenantID, start, end)
}

func assessUserRegistration(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	return assessUserAuthentication(ctx, r, tenantID, start, end)
}

func assessEventLogging(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	return assessSystemMonitoring(ctx, r, tenantID, start, end)
}

func assessAdminLogging(ctx context.Context, r *ComplianceReporter, tenantID uuid.UUID, start, end time.Time) (ControlStatus, []Finding, error) {
	return assessSystemMonitoring(ctx, r, tenantID, start, end)
}

// ExportReport exports a compliance report in the specified format
func (r *ComplianceReporter) ExportReport(w io.Writer, report *ComplianceReport, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "html":
		return r.exportReportHTML(w, report)
	case "pdf":
		return fmt.Errorf("PDF export requires external library integration")
	default:
		return fmt.Errorf("unsupported report format: %s", format)
	}
}

// exportReportHTML exports report as HTML
func (r *ComplianceReporter) exportReportHTML(w io.Writer, report *ComplianceReport) error {
	// Simple HTML template
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Compliance Report - %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        h1 { color: #333; }
        .summary { background: #f5f5f5; padding: 20px; border-radius: 5px; }
        .metric { display: inline-block; margin: 10px 20px; }
        .metric-value { font-size: 24px; font-weight: bold; }
        .control { border: 1px solid #ddd; margin: 10px 0; padding: 15px; }
        .passing { border-left: 4px solid #28a745; }
        .failing { border-left: 4px solid #dc3545; }
        .partial { border-left: 4px solid #ffc107; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 10px; border: 1px solid #ddd; text-align: left; }
        th { background: #f5f5f5; }
    </style>
</head>
<body>
    <h1>%s Compliance Report</h1>
    <p>Period: %s to %s</p>
    <p>Generated: %s</p>

    <div class="summary">
        <h2>Executive Summary</h2>
        <div class="metric">
            <div class="metric-value">%.1f%%</div>
            <div>Compliance Score</div>
        </div>
        <div class="metric">
            <div class="metric-value">%d/%d</div>
            <div>Controls Passing</div>
        </div>
        <div class="metric">
            <div class="metric-value">%s</div>
            <div>Risk Level</div>
        </div>
    </div>

    <h2>Key Metrics</h2>
    <table>
        <tr><th>Metric</th><th>Value</th></tr>
        <tr><td>Audit Trail Completeness</td><td>%.2f%%</td></tr>
        <tr><td>Policy Enforcement Rate</td><td>%.2f%%</td></tr>
        <tr><td>Avg Escalation Response Time</td><td>%.2f hours</td></tr>
        <tr><td>Sensitive Access Events</td><td>%d</td></tr>
        <tr><td>Unauthorized Attempts</td><td>%d</td></tr>
    </table>

    <h2>Control Assessments</h2>
`,
		report.Framework,
		report.Framework,
		report.PeriodStart.Format("2006-01-02"),
		report.PeriodEnd.Format("2006-01-02"),
		report.GeneratedAt.Format("2006-01-02 15:04:05"),
		report.Summary.ComplianceScore,
		report.Summary.PassingControls,
		report.Summary.TotalControls,
		report.Summary.RiskLevel,
		report.Summary.AuditTrailCompleteness,
		report.Summary.PolicyEnforcement,
		report.Summary.EscalationResponseTime,
		report.Summary.SensitiveAccessEvents,
		report.Summary.UnauthorizedAttempts,
	)

	// Add controls
	for _, control := range report.Controls {
		class := "control "
		switch control.Status {
		case ControlPassing:
			class += "passing"
		case ControlFailing:
			class += "failing"
		default:
			class += "partial"
		}

		html += fmt.Sprintf(`
    <div class="%s">
        <h3>%s - %s</h3>
        <p><strong>Status:</strong> %s</p>
        <p>%s</p>
    </div>
`, class, control.ControlID, control.ControlName, control.Status, control.Description)
	}

	html += `
</body>
</html>`

	_, err := w.Write([]byte(html))
	return err
}
