package docengine

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/adp/adp/internal/store"
)

// SessionAnalysis contains computed statistics from a session's decision records.
type SessionAnalysis struct {
	SessionID        string
	Tool             string
	TrustLevel       int
	Duration         time.Duration
	StartedAt        time.Time
	DecisionCount    int
	DecisionsByType  map[string]int
	AvgConfidence    float64
	MinConfidence    float64
	FilesTouched     []string
	PolicyViolations int
	DeniedDecisions  int
	Outcomes         map[string]int
}

// AnalyzeSession computes statistics from a session and its decision records.
func AnalyzeSession(session *store.Session, decisions []*store.DecisionRecord) SessionAnalysis {
	analysis := SessionAnalysis{
		SessionID:       session.ID,
		Tool:            session.Tool,
		TrustLevel:      session.TrustLevel,
		StartedAt:       session.StartedAt,
		DecisionCount:   len(decisions),
		DecisionsByType: make(map[string]int),
		MinConfidence:   1.0,
		Outcomes:        make(map[string]int),
	}

	if session.LastHeartbeat != nil {
		analysis.Duration = session.LastHeartbeat.Sub(session.StartedAt)
	} else {
		analysis.Duration = time.Since(session.StartedAt)
	}

	filesSet := make(map[string]bool)
	var totalConfidence float64

	for _, d := range decisions {
		analysis.DecisionsByType[d.DecisionType]++
		totalConfidence += d.Confidence
		if d.Confidence < analysis.MinConfidence {
			analysis.MinConfidence = d.Confidence
		}

		analysis.Outcomes[d.Status]++

		// Extract files from target
		if paths, ok := d.Target["paths"].([]interface{}); ok {
			for _, p := range paths {
				if s, ok := p.(string); ok {
					filesSet[s] = true
				}
			}
		}
		if path, ok := d.Target["path"].(string); ok {
			filesSet[path] = true
		}

		// Count policy violations
		if d.PolicyResult != nil && !d.PolicyResult.Allowed {
			analysis.PolicyViolations++
			analysis.DeniedDecisions++
		}
	}

	if len(decisions) > 0 {
		analysis.AvgConfidence = totalConfidence / float64(len(decisions))
	}

	for f := range filesSet {
		analysis.FilesTouched = append(analysis.FilesTouched, f)
	}
	sort.Strings(analysis.FilesTouched)

	return analysis
}

// TemplateEngine renders session analysis into Markdown documentation.
type TemplateEngine struct {
	sessionSummary *template.Template
	riskReport     *template.Template
	patternReport  *template.Template
}

// NewTemplateEngine creates a template engine with embedded templates.
func NewTemplateEngine() (*TemplateEngine, error) {
	funcMap := template.FuncMap{
		"printf": fmt.Sprintf,
		"durationMinutes": func(d time.Duration) int {
			return int(d.Minutes())
		},
	}

	ss, err := template.New("session_summary").Funcs(funcMap).Parse(sessionSummaryTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse session summary template: %w", err)
	}

	rr, err := template.New("risk_report").Funcs(funcMap).Parse(riskReportTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse risk report template: %w", err)
	}

	pr, err := template.New("pattern_report").Funcs(funcMap).Parse(patternReportTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pattern report template: %w", err)
	}

	return &TemplateEngine{
		sessionSummary: ss,
		riskReport:     rr,
		patternReport:  pr,
	}, nil
}

// RenderSessionSummary generates a Markdown session summary.
func (e *TemplateEngine) RenderSessionSummary(analysis SessionAnalysis) (string, error) {
	return render(e.sessionSummary, analysis)
}

// RenderRiskReport generates a Markdown risk report.
func (e *TemplateEngine) RenderRiskReport(analysis SessionAnalysis) (string, error) {
	return render(e.riskReport, analysis)
}

// RenderPatternReport generates a Markdown pattern report.
func (e *TemplateEngine) RenderPatternReport(analysis SessionAnalysis) (string, error) {
	return render(e.patternReport, analysis)
}

func render(t *template.Template, data interface{}) (string, error) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// Templates

const sessionSummaryTemplate = `# Session Summary: {{.SessionID}}

**Agent**: {{.Tool}} | **Trust Level**: {{.TrustLevel}} | **Duration**: {{durationMinutes .Duration}}m

## Decisions Made: {{.DecisionCount}}

| Type | Count |
|------|-------|
{{- range $type, $count := .DecisionsByType}}
| {{$type}} | {{$count}} |
{{- end}}

## Confidence
- Average: {{printf "%.2f" .AvgConfidence}}
- Minimum: {{printf "%.2f" .MinConfidence}}

{{- if gt (len .FilesTouched) 0}}

## Files Touched ({{len .FilesTouched}})
{{range .FilesTouched}}- ` + "`{{.}}`" + `
{{end}}
{{- end}}

{{- if gt .PolicyViolations 0}}

## Policy Violations: {{.PolicyViolations}}
Action required: review denied decisions for compliance.
{{- end}}
`

const riskReportTemplate = `# Risk Report: {{.SessionID}}

**Generated**: {{.StartedAt.Format "2006-01-02 15:04 UTC"}}

## Risk Indicators

| Indicator | Value | Status |
|-----------|-------|--------|
| Min Confidence | {{printf "%.2f" .MinConfidence}} | {{if lt .MinConfidence 0.5}}HIGH RISK{{else if lt .MinConfidence 0.7}}MEDIUM{{else}}OK{{end}} |
| Policy Violations | {{.PolicyViolations}} | {{if gt .PolicyViolations 0}}REVIEW REQUIRED{{else}}CLEAN{{end}} |
| Decision Volume | {{.DecisionCount}} | {{if gt .DecisionCount 100}}HIGH{{else if gt .DecisionCount 50}}MODERATE{{else}}NORMAL{{end}} |

{{- if gt .PolicyViolations 0}}

## Denied Actions
{{.DeniedDecisions}} decision(s) were denied by policy. Review the audit trail for details.
{{- end}}

{{- if lt .MinConfidence 0.5}}

## Low Confidence Alert
One or more decisions were made with confidence below 0.5. These should be reviewed by a human.
{{- end}}
`

const patternReportTemplate = `# Pattern Report: {{.SessionID}}

**Agent**: {{.Tool}} | **Decisions**: {{.DecisionCount}}

## Decision Type Distribution

{{- range $type, $count := .DecisionsByType}}
- **{{$type}}**: {{$count}} occurrence(s)
{{- end}}

## Session Profile
- Duration: {{durationMinutes .Duration}} minutes
- Files touched: {{len .FilesTouched}}
- Average confidence: {{printf "%.2f" .AvgConfidence}}
- Trust level: {{.TrustLevel}}

{{- if gt (len .FilesTouched) 10}}

## High File Impact
This session touched {{len .FilesTouched}} files. Consider whether the scope was appropriate for the trust level.
{{- end}}
`
