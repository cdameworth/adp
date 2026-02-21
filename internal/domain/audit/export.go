// Package audit provides audit logging and export functionality for ADP.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ExportFormat represents supported export formats
type ExportFormat string

const (
	FormatJSON   ExportFormat = "json"
	FormatJSONL  ExportFormat = "jsonl" // JSON Lines (one JSON object per line)
	FormatCSV    ExportFormat = "csv"
	FormatCEF    ExportFormat = "cef"    // Common Event Format (ArcSight)
	FormatLEEF   ExportFormat = "leef"   // Log Event Extended Format (QRadar)
	FormatSyslog ExportFormat = "syslog" // RFC 5424
)

// AuditEvent represents a normalized audit event for export
type AuditEvent struct {
	// Core fields
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	EventType     string    `json:"event_type"`
	EventCategory string    `json:"event_category"`
	Severity      string    `json:"severity"`

	// Actor
	ActorID    string `json:"actor_id"`
	ActorType  string `json:"actor_type"` // user, agent, system
	ActorEmail string `json:"actor_email,omitempty"`
	SessionID  string `json:"session_id,omitempty"`

	// Organization context
	TenantID       string `json:"tenant_id"`
	OrganizationID string `json:"organization_id"`

	// Action details
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	ResourceID string `json:"resource_id,omitempty"`
	Result     string `json:"result"` // success, failure, error

	// Policy evaluation (if applicable)
	PolicyName   string `json:"policy_name,omitempty"`
	PolicyResult string `json:"policy_result,omitempty"`
	TrustLevel   int    `json:"trust_level,omitempty"`

	// Additional context
	Reasoning   string            `json:"reasoning,omitempty"`
	TargetPaths []string          `json:"target_paths,omitempty"`
	BlastRadius int               `json:"blast_radius,omitempty"`
	Confidence  float64           `json:"confidence,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	// Source information
	SourceIP  string `json:"source_ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	RequestID string `json:"request_id,omitempty"`

	// Compliance markers
	DataClassification string   `json:"data_classification,omitempty"`
	ComplianceFlags    []string `json:"compliance_flags,omitempty"`
}

// ExportFilter defines criteria for exporting audit events
type ExportFilter struct {
	TenantID        uuid.UUID
	OrganizationID  *uuid.UUID
	StartTime       time.Time
	EndTime         time.Time
	EventTypes      []string
	EventCategories []string
	ActorIDs        []string
	SessionIDs      []string
	Actions         []string
	Results         []string
	MinSeverity     string
	Limit           int
	Offset          int
}

// AuditExporter exports audit events in various formats
type AuditExporter struct {
	store  AuditEventStore
	config ExportConfig
}

// ExportConfig holds configuration for the audit exporter
type ExportConfig struct {
	// DefaultFormat is the format to use when not specified
	DefaultFormat ExportFormat
	// IncludeMetadata whether to include metadata in exports
	IncludeMetadata bool
	// MaxEventsPerExport limits the number of events per export
	MaxEventsPerExport int
	// CEFVendor is the vendor name for CEF format
	CEFVendor string
	// CEFProduct is the product name for CEF format
	CEFProduct string
	// CEFVersion is the CEF version
	CEFVersion string
	// SyslogFacility for syslog format
	SyslogFacility int
	// SyslogAppName for syslog format
	SyslogAppName string
}

// AuditEventStore interface for fetching audit events
type AuditEventStore interface {
	ListEvents(ctx context.Context, filter ExportFilter) ([]*AuditEvent, error)
	CountEvents(ctx context.Context, filter ExportFilter) (int64, error)
}

// NewAuditExporter creates a new audit exporter
func NewAuditExporter(store AuditEventStore, config ExportConfig) *AuditExporter {
	if config.DefaultFormat == "" {
		config.DefaultFormat = FormatJSONL
	}
	if config.MaxEventsPerExport == 0 {
		config.MaxEventsPerExport = 100000
	}
	if config.CEFVendor == "" {
		config.CEFVendor = "ADP"
	}
	if config.CEFProduct == "" {
		config.CEFProduct = "Agent Developer Portal"
	}
	if config.CEFVersion == "" {
		config.CEFVersion = "1.0"
	}
	if config.SyslogFacility == 0 {
		config.SyslogFacility = 16 // local0
	}
	if config.SyslogAppName == "" {
		config.SyslogAppName = "adp"
	}
	return &AuditExporter{
		store:  store,
		config: config,
	}
}

// Export exports audit events to the specified writer in the specified format
func (e *AuditExporter) Export(ctx context.Context, w io.Writer, filter ExportFilter, format ExportFormat) error {
	if format == "" {
		format = e.config.DefaultFormat
	}

	// Enforce max events limit
	if filter.Limit == 0 || filter.Limit > e.config.MaxEventsPerExport {
		filter.Limit = e.config.MaxEventsPerExport
	}

	events, err := e.store.ListEvents(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to fetch audit events: %w", err)
	}

	switch format {
	case FormatJSON:
		return e.exportJSON(w, events)
	case FormatJSONL:
		return e.exportJSONL(w, events)
	case FormatCSV:
		return e.exportCSV(w, events)
	case FormatCEF:
		return e.exportCEF(w, events)
	case FormatLEEF:
		return e.exportLEEF(w, events)
	case FormatSyslog:
		return e.exportSyslog(w, events)
	default:
		return fmt.Errorf("unsupported export format: %s", format)
	}
}

// exportJSON exports events as a single JSON array
func (e *AuditExporter) exportJSON(w io.Writer, events []*AuditEvent) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(events)
}

// exportJSONL exports events as JSON Lines (one JSON object per line)
func (e *AuditExporter) exportJSONL(w io.Writer, events []*AuditEvent) error {
	encoder := json.NewEncoder(w)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

// exportCSV exports events as CSV
func (e *AuditExporter) exportCSV(w io.Writer, events []*AuditEvent) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	header := []string{
		"id", "timestamp", "event_type", "event_category", "severity",
		"actor_id", "actor_type", "session_id",
		"tenant_id", "organization_id",
		"action", "resource", "resource_id", "result",
		"policy_name", "policy_result", "trust_level",
		"confidence", "source_ip", "request_id",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write events
	for _, event := range events {
		row := []string{
			event.ID,
			event.Timestamp.Format(time.RFC3339),
			event.EventType,
			event.EventCategory,
			event.Severity,
			event.ActorID,
			event.ActorType,
			event.SessionID,
			event.TenantID,
			event.OrganizationID,
			event.Action,
			event.Resource,
			event.ResourceID,
			event.Result,
			event.PolicyName,
			event.PolicyResult,
			fmt.Sprintf("%d", event.TrustLevel),
			fmt.Sprintf("%.2f", event.Confidence),
			event.SourceIP,
			event.RequestID,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// exportCEF exports events in Common Event Format (ArcSight)
// Format: CEF:Version|Device Vendor|Device Product|Device Version|Signature ID|Name|Severity|Extension
func (e *AuditExporter) exportCEF(w io.Writer, events []*AuditEvent) error {
	for _, event := range events {
		severity := e.mapSeverityToCEF(event.Severity)
		signatureID := fmt.Sprintf("%s-%s", event.EventCategory, event.EventType)
		name := fmt.Sprintf("%s on %s", event.Action, event.Resource)

		// Build extension
		ext := []string{
			fmt.Sprintf("rt=%d", event.Timestamp.UnixMilli()),
			fmt.Sprintf("dvchost=%s", e.config.CEFProduct),
			fmt.Sprintf("suser=%s", escapeKeyValue(event.ActorID)),
			fmt.Sprintf("outcome=%s", event.Result),
		}

		if event.SourceIP != "" {
			ext = append(ext, fmt.Sprintf("src=%s", event.SourceIP))
		}
		if event.ResourceID != "" {
			ext = append(ext, fmt.Sprintf("cs1=%s", escapeKeyValue(event.ResourceID)))
			ext = append(ext, "cs1Label=ResourceID")
		}
		if event.SessionID != "" {
			ext = append(ext, fmt.Sprintf("cs2=%s", escapeKeyValue(event.SessionID)))
			ext = append(ext, "cs2Label=SessionID")
		}
		if event.TenantID != "" {
			ext = append(ext, fmt.Sprintf("cs3=%s", escapeKeyValue(event.TenantID)))
			ext = append(ext, "cs3Label=TenantID")
		}
		if event.OrganizationID != "" {
			ext = append(ext, fmt.Sprintf("cs4=%s", escapeKeyValue(event.OrganizationID)))
			ext = append(ext, "cs4Label=OrganizationID")
		}
		if event.PolicyName != "" {
			ext = append(ext, fmt.Sprintf("cs5=%s", escapeKeyValue(event.PolicyName)))
			ext = append(ext, "cs5Label=PolicyName")
		}
		if event.TrustLevel > 0 {
			ext = append(ext, fmt.Sprintf("cn1=%d", event.TrustLevel))
			ext = append(ext, "cn1Label=TrustLevel")
		}

		// Write CEF line
		line := fmt.Sprintf("CEF:0|%s|%s|%s|%s|%s|%d|%s\n",
			e.config.CEFVendor,
			e.config.CEFProduct,
			e.config.CEFVersion,
			signatureID,
			escapeKeyValue(name),
			severity,
			strings.Join(ext, " "),
		)

		if _, err := w.Write([]byte(line)); err != nil {
			return err
		}
	}

	return nil
}

// exportLEEF exports events in Log Event Extended Format (IBM QRadar)
// Format: LEEF:Version|Vendor|Product|Version|EventID|Extension
func (e *AuditExporter) exportLEEF(w io.Writer, events []*AuditEvent) error {
	for _, event := range events {
		eventID := fmt.Sprintf("%s.%s", event.EventCategory, event.EventType)

		// Build extension (tab-separated key=value pairs)
		ext := []string{
			fmt.Sprintf("devTime=%s", event.Timestamp.Format(time.RFC3339)),
			fmt.Sprintf("usrName=%s", event.ActorID),
			fmt.Sprintf("cat=%s", event.EventCategory),
			fmt.Sprintf("severity=%s", event.Severity),
			fmt.Sprintf("action=%s", event.Action),
			fmt.Sprintf("resource=%s", event.Resource),
			fmt.Sprintf("result=%s", event.Result),
		}

		if event.SourceIP != "" {
			ext = append(ext, fmt.Sprintf("src=%s", event.SourceIP))
		}
		if event.SessionID != "" {
			ext = append(ext, fmt.Sprintf("sessionID=%s", event.SessionID))
		}
		if event.TenantID != "" {
			ext = append(ext, fmt.Sprintf("tenantID=%s", event.TenantID))
		}
		if event.OrganizationID != "" {
			ext = append(ext, fmt.Sprintf("orgID=%s", event.OrganizationID))
		}
		if event.PolicyName != "" {
			ext = append(ext, fmt.Sprintf("policy=%s", event.PolicyName))
		}

		// Write LEEF line
		line := fmt.Sprintf("LEEF:2.0|%s|%s|%s|%s|%s\n",
			e.config.CEFVendor,
			e.config.CEFProduct,
			e.config.CEFVersion,
			eventID,
			strings.Join(ext, "\t"),
		)

		if _, err := w.Write([]byte(line)); err != nil {
			return err
		}
	}

	return nil
}

// exportSyslog exports events in RFC 5424 syslog format
func (e *AuditExporter) exportSyslog(w io.Writer, events []*AuditEvent) error {
	for _, event := range events {
		priority := e.calculateSyslogPriority(event.Severity)
		timestamp := event.Timestamp.Format(time.RFC3339)
		hostname := "-"
		msgID := fmt.Sprintf("%s.%s", event.EventCategory, event.EventType)

		// Build structured data
		sd := fmt.Sprintf("[adp@%d actor=\"%s\" session=\"%s\" tenant=\"%s\" org=\"%s\" action=\"%s\" result=\"%s\"]",
			32473, // Private Enterprise Number (example)
			escapeSD(event.ActorID),
			escapeSD(event.SessionID),
			escapeSD(event.TenantID),
			escapeSD(event.OrganizationID),
			escapeSD(event.Action),
			escapeSD(event.Result),
		)

		// Build message
		msg := fmt.Sprintf("%s on %s by %s", event.Action, event.Resource, event.ActorType)

		// RFC 5424 format: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
		line := fmt.Sprintf("<%d>1 %s %s %s - %s %s %s\n",
			priority,
			timestamp,
			hostname,
			e.config.SyslogAppName,
			msgID,
			sd,
			msg,
		)

		if _, err := w.Write([]byte(line)); err != nil {
			return err
		}
	}

	return nil
}

// mapSeverityToCEF maps ADP severity to CEF severity (0-10)
func (e *AuditExporter) mapSeverityToCEF(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 10
	case "high":
		return 8
	case "medium":
		return 5
	case "low":
		return 3
	case "info", "informational":
		return 1
	default:
		return 0
	}
}

// calculateSyslogPriority calculates syslog priority from facility and severity
func (e *AuditExporter) calculateSyslogPriority(severity string) int {
	var severityNum int
	switch strings.ToLower(severity) {
	case "critical":
		severityNum = 2 // Critical
	case "high":
		severityNum = 3 // Error
	case "medium":
		severityNum = 4 // Warning
	case "low":
		severityNum = 5 // Notice
	case "info", "informational":
		severityNum = 6 // Informational
	default:
		severityNum = 7 // Debug
	}
	return (e.config.SyslogFacility * 8) + severityNum
}

// escapeKeyValue escapes special characters for CEF format
func escapeKeyValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "=", "\\=")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// escapeSD escapes special characters for syslog structured data
func escapeSD(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "]", "\\]")
	return s
}

// StreamExport exports events in a streaming fashion for large exports
type StreamExport struct {
	exporter  *AuditExporter
	filter    ExportFilter
	format    ExportFormat
	writer    io.Writer
	batchSize int
}

// NewStreamExport creates a new streaming export
func (e *AuditExporter) NewStreamExport(w io.Writer, filter ExportFilter, format ExportFormat) *StreamExport {
	return &StreamExport{
		exporter:  e,
		filter:    filter,
		format:    format,
		writer:    w,
		batchSize: 1000,
	}
}

// Stream exports events in batches
func (s *StreamExport) Stream(ctx context.Context) error {
	offset := 0
	for {
		s.filter.Offset = offset
		s.filter.Limit = s.batchSize

		events, err := s.exporter.store.ListEvents(ctx, s.filter)
		if err != nil {
			return err
		}

		if len(events) == 0 {
			break
		}

		// Export this batch
		switch s.format {
		case FormatJSONL:
			if err := s.exporter.exportJSONL(s.writer, events); err != nil {
				return err
			}
		case FormatCSV:
			if offset == 0 {
				// Write header for first batch
				csvWriter := csv.NewWriter(s.writer)
				header := []string{
					"id", "timestamp", "event_type", "event_category", "severity",
					"actor_id", "actor_type", "session_id",
					"tenant_id", "organization_id",
					"action", "resource", "resource_id", "result",
				}
				if err := csvWriter.Write(header); err != nil {
					return err
				}
				csvWriter.Flush()
			}
			if err := s.exporter.exportCSV(s.writer, events); err != nil {
				return err
			}
		case FormatCEF:
			if err := s.exporter.exportCEF(s.writer, events); err != nil {
				return err
			}
		case FormatLEEF:
			if err := s.exporter.exportLEEF(s.writer, events); err != nil {
				return err
			}
		case FormatSyslog:
			if err := s.exporter.exportSyslog(s.writer, events); err != nil {
				return err
			}
		default:
			return fmt.Errorf("streaming not supported for format: %s", s.format)
		}

		if len(events) < s.batchSize {
			break
		}

		offset += len(events)
	}

	return nil
}
