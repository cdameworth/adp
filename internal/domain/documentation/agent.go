package docengine

import (
	"context"
	"log"
	"time"

	"github.com/adp/adp/internal/store"
	"github.com/google/uuid"
)

// DocAgent is a background process that watches for completed sessions and
// generates curated documentation from their decision records.
//
// It runs as a goroutine within the MCP server process, polling for ended
// sessions that don't yet have documentation. For each session, it:
//  1. Fetches all decisions via DecisionStore.ListBySession
//  2. Analyzes decisions (confidence, files, policy outcomes)
//  3. Generates docs via templates (default) or LLM (if configured)
//  4. Saves documentation to DocStore
type DocAgent struct {
	decisionStore store.DecisionStore
	sessionStore  store.SessionStore
	docStore      store.DocStore
	templates     *TemplateEngine
	llm           LLMClient
	interval      time.Duration
	logger        *log.Logger
}

// DocAgentConfig configures the documentation agent.
type DocAgentConfig struct {
	Interval  time.Duration // Polling interval (default: 30s)
	LLMAPIKey string        // Optional: Anthropic API key for LLM-enhanced docs
	LLMModel  string        // Optional: model name (default: claude-sonnet-4-20250514)
}

// NewDocAgent creates a new documentation agent.
func NewDocAgent(
	ds store.DecisionStore,
	ss store.SessionStore,
	docs store.DocStore,
	cfg DocAgentConfig,
) (*DocAgent, error) {
	tmpl, err := NewTemplateEngine()
	if err != nil {
		return nil, err
	}

	interval := cfg.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	model := cfg.LLMModel
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}

	return &DocAgent{
		decisionStore: ds,
		sessionStore:  ss,
		docStore:      docs,
		templates:     tmpl,
		llm:           NewLLMClient(cfg.LLMAPIKey, model),
		interval:      interval,
		logger:        log.New(log.Writer(), "[doc-agent] ", log.LstdFlags),
	}, nil
}

// Start begins the background polling loop. It blocks until ctx is canceled.
func (a *DocAgent) Start(ctx context.Context) {
	a.logger.Println("Documentation agent started")

	// Process any existing ended sessions immediately
	a.processNewSessions(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Println("Documentation agent stopped")
			return
		case <-ticker.C:
			a.processNewSessions(ctx)
		}
	}
}

// processNewSessions finds ended sessions and generates docs for those
// that don't already have documentation.
func (a *DocAgent) processNewSessions(ctx context.Context) {
	sessions, err := a.sessionStore.ListEnded(ctx, "", 50)
	if err != nil {
		a.logger.Printf("Failed to list ended sessions: %v", err)
		return
	}

	for _, session := range sessions {
		// Check if docs already exist for this session (idempotency)
		existingDocs, err := a.docStore.ListBySession(ctx, session.ID)
		if err != nil {
			a.logger.Printf("Failed to check existing docs for session %s: %v", session.ID, err)
			continue
		}
		if len(existingDocs) > 0 {
			continue // Already processed
		}

		if err := a.ProcessSession(ctx, session); err != nil {
			a.logger.Printf("Failed to process session %s: %v", session.ID, err)
		}
	}
}

// ProcessSession generates documentation for a single ended session.
func (a *DocAgent) ProcessSession(ctx context.Context, session *store.Session) error {
	decisions, err := a.decisionStore.ListBySession(ctx, session.ID)
	if err != nil {
		return err
	}

	if len(decisions) == 0 {
		a.logger.Printf("Session %s has no decisions, skipping", session.ID)
		return nil
	}

	analysis := AnalyzeSession(session, decisions)

	// Generate session summary (always)
	summaryContent, err := a.templates.RenderSessionSummary(analysis)
	if err != nil {
		return err
	}

	// Try LLM refinement if configured
	if a.llm.IsConfigured() {
		refined, err := a.llm.GenerateDoc(ctx,
			"Refine this session summary into clear, concise technical documentation.",
			summaryContent, analysis,
		)
		if err != nil {
			a.logger.Printf("LLM refinement failed for session %s, using template: %v", session.ID, err)
		} else if refined != "" {
			summaryContent = refined
		}
	}

	if err := a.docStore.Save(ctx, store.DocRecord{
		ID:        uuid.New().String(),
		SessionID: session.ID,
		Category:  "session_summary",
		Title:     "Session Summary: " + session.ID,
		Content:   summaryContent,
		Metadata: map[string]any{
			"tool":           session.Tool,
			"trust_level":    session.TrustLevel,
			"decision_count": len(decisions),
			"avg_confidence": analysis.AvgConfidence,
			"llm_enhanced":   a.llm.IsConfigured(),
		},
	}); err != nil {
		return err
	}

	// Generate risk report only if there are concerns
	if analysis.PolicyViolations > 0 || analysis.MinConfidence < 0.7 {
		riskContent, err := a.templates.RenderRiskReport(analysis)
		if err != nil {
			return err
		}

		if a.llm.IsConfigured() {
			refined, err := a.llm.GenerateDoc(ctx,
				"Refine this risk report with actionable recommendations.",
				riskContent, analysis,
			)
			if err == nil && refined != "" {
				riskContent = refined
			}
		}

		if err := a.docStore.Save(ctx, store.DocRecord{
			ID:        uuid.New().String(),
			SessionID: session.ID,
			Category:  "risk_report",
			Title:     "Risk Report: " + session.ID,
			Content:   riskContent,
		}); err != nil {
			return err
		}
	}

	// Generate pattern report for sessions with many decisions
	if len(decisions) >= 5 {
		patternContent, err := a.templates.RenderPatternReport(analysis)
		if err != nil {
			return err
		}

		if err := a.docStore.Save(ctx, store.DocRecord{
			ID:        uuid.New().String(),
			SessionID: session.ID,
			Category:  "pattern_report",
			Title:     "Pattern Report: " + session.ID,
			Content:   patternContent,
		}); err != nil {
			return err
		}
	}

	a.logger.Printf("Generated docs for session %s (%d decisions)", session.ID, len(decisions))
	return nil
}
