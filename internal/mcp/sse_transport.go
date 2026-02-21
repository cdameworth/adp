// Package mcp provides SSE (Server-Sent Events) transport for the MCP server.
// This enables HTTP-based communication with AI agents that support SSE transport
// instead of stdio.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// SSETransport implements HTTP SSE transport for MCP
type SSETransport struct {
	server     *Server
	sessions   map[string]*SSESession
	sessionsMu sync.RWMutex
}

// SSESession represents an active SSE connection
type SSESession struct {
	id         string
	writer     http.ResponseWriter
	flusher    http.Flusher
	messages   chan json.RawMessage
	done       chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	lastActive time.Time
}

// NewSSETransport creates a new SSE transport for the given MCP server
func NewSSETransport(server *Server) *SSETransport {
	return &SSETransport{
		server:   server,
		sessions: make(map[string]*SSESession),
	}
}

// Handler returns an HTTP handler for the SSE endpoint
func (t *SSETransport) Handler() http.Handler {
	mux := http.NewServeMux()

	// SSE endpoint for receiving events
	mux.HandleFunc("GET /sse", t.handleSSE)

	// Message endpoint for sending JSON-RPC requests
	mux.HandleFunc("POST /message", t.handleMessage)

	// Health check
	mux.HandleFunc("GET /health", t.handleHealth)

	return mux
}

// handleSSE handles the SSE connection
func (t *SSETransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Check for SSE support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Get or generate session ID
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = fmt.Sprintf("sse-%d", time.Now().UnixNano())
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ctx, cancel := context.WithCancel(r.Context())
	session := &SSESession{
		id:         sessionID,
		writer:     w,
		flusher:    flusher,
		messages:   make(chan json.RawMessage, 100),
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		lastActive: time.Now(),
	}

	// Register session
	t.sessionsMu.Lock()
	t.sessions[sessionID] = session
	t.sessionsMu.Unlock()

	defer func() {
		t.sessionsMu.Lock()
		delete(t.sessions, sessionID)
		t.sessionsMu.Unlock()
		close(session.done)
		cancel()
	}()

	// Send initial connection event
	t.sendEvent(session, "connected", map[string]string{
		"session_id": sessionID,
		"server":     ServerName,
		"version":    ServerVersion,
		"protocol":   ProtocolVersion,
	})

	// Start heartbeat goroutine
	go t.heartbeatLoop(session)

	// Wait for messages or context cancellation
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-session.messages:
			t.sendEvent(session, "message", msg)
		}
	}
}

// handleMessage handles incoming JSON-RPC messages via POST
func (t *SSETransport) handleMessage(w http.ResponseWriter, r *http.Request) {
	// Get session ID from header or query
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session_id")
	}

	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse JSON-RPC request
	var request JSONRPCRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.sendJSONRPCError(w, nil, ErrCodeParse, "Parse error")
		return
	}

	// Handle the request
	response := t.server.handleRequest(r.Context(), &request)

	// Send response via SSE if session exists
	t.sessionsMu.RLock()
	session, exists := t.sessions[sessionID]
	t.sessionsMu.RUnlock()

	if exists && response != nil {
		responseJSON, _ := json.Marshal(response)
		select {
		case session.messages <- responseJSON:
			session.lastActive = time.Now()
		default:
			// Channel full, log warning
		}
	}

	// Also return response directly for clients that expect it
	if response != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleHealth returns server health status
func (t *SSETransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	t.sessionsMu.RLock()
	sessionCount := len(t.sessions)
	t.sessionsMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"server":          ServerName,
		"version":         ServerVersion,
		"protocol":        ProtocolVersion,
		"active_sessions": sessionCount,
	})
}

// sendEvent sends an SSE event to a session
func (t *SSETransport) sendEvent(session *SSESession, event string, data interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Format as SSE event
	_, err = fmt.Fprintf(session.writer, "event: %s\ndata: %s\n\n", event, string(dataJSON))
	if err != nil {
		return err
	}

	session.flusher.Flush()
	return nil
}

// sendJSONRPCError sends a JSON-RPC error response
func (t *SSETransport) sendJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(&JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	})
}

// heartbeatLoop sends periodic heartbeats to keep the connection alive
func (t *SSETransport) heartbeatLoop(session *SSESession) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-session.ctx.Done():
			return
		case <-ticker.C:
			t.sendEvent(session, "heartbeat", map[string]interface{}{
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

// Broadcast sends a message to all connected sessions
func (t *SSETransport) Broadcast(event string, data interface{}) {
	t.sessionsMu.RLock()
	defer t.sessionsMu.RUnlock()

	for _, session := range t.sessions {
		go t.sendEvent(session, event, data)
	}
}

// GetSessionCount returns the number of active SSE sessions
func (t *SSETransport) GetSessionCount() int {
	t.sessionsMu.RLock()
	defer t.sessionsMu.RUnlock()
	return len(t.sessions)
}

// CloseSession closes a specific SSE session
func (t *SSETransport) CloseSession(sessionID string) {
	t.sessionsMu.Lock()
	session, exists := t.sessions[sessionID]
	if exists {
		delete(t.sessions, sessionID)
	}
	t.sessionsMu.Unlock()

	if exists {
		session.cancel()
	}
}

// SSEReader implements io.Reader for reading from SSE-style input
// This allows the standard MCP server to work with HTTP-based input
type SSEReader struct {
	reader *bufio.Reader
}

// NewSSEReader creates a new SSE reader
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{
		reader: bufio.NewReader(r),
	}
}

// Read implements io.Reader, extracting data from SSE format
func (r *SSEReader) Read(p []byte) (n int, err error) {
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}

		// Skip empty lines and event lines
		if line == "\n" || len(line) == 0 {
			continue
		}

		// Look for data: prefix
		if len(line) > 5 && line[:5] == "data:" {
			data := line[5:]
			// Trim leading space and trailing newline
			if len(data) > 0 && data[0] == ' ' {
				data = data[1:]
			}
			data = data[:len(data)-1] // Remove trailing newline

			return copy(p, data), nil
		}
	}
}

// SSEWriter implements io.Writer for writing in SSE format
type SSEWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

// NewSSEWriter creates a new SSE writer
func NewSSEWriter(w io.Writer, flusher http.Flusher) *SSEWriter {
	return &SSEWriter{
		writer:  w,
		flusher: flusher,
	}
}

// Write implements io.Writer, formatting output as SSE events
func (w *SSEWriter) Write(p []byte) (n int, err error) {
	// Write as SSE data event
	written, err := fmt.Fprintf(w.writer, "data: %s\n\n", string(p))
	if err != nil {
		return 0, err
	}

	if w.flusher != nil {
		w.flusher.Flush()
	}

	return written, nil
}
