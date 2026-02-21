package audit

import (
	"encoding/json"
	"log"
	"os"
)

// AuditLogger defines the interface for logging decision records
type AuditLogger interface {
	LogDecision(record DecisionRecord) error
}

// StandardLogger implements AuditLogger using standard log package (or structured logger)
type StandardLogger struct {
	logger *log.Logger
}

// NewStandardLogger creates a new StandardLogger
func NewStandardLogger() *StandardLogger {
	return &StandardLogger{
		logger: log.New(os.Stdout, "[AUDIT] ", log.LstdFlags),
	}
}

// LogDecision logs a decision record
func (l *StandardLogger) LogDecision(record DecisionRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	l.logger.Println(string(data))
	return nil
}
