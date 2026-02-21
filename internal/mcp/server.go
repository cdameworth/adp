// Package mcp implements the Model Context Protocol (MCP) server for ADP.
// The MCP server enables AI agents to interact with ADP for governance,
// context delivery, and audit trail management.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/adp/adp/internal/store"
)

// Protocol version
const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "adp-mcp-server"
	ServerVersion   = "1.0.0"
)

// JSON-RPC types
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error codes
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// MCP types
type InitializeParams struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ClientCapability `json:"capabilities"`
	ClientInfo      ClientInfo       `json:"clientInfo"`
}

type ClientCapability struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string           `json:"protocolVersion"`
	Capabilities    ServerCapability `json:"capabilities"`
	ServerInfo      ServerInfo       `json:"serverInfo"`
}

type ServerCapability struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	Logging   *LoggingCapability   `json:"logging,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Server represents the MCP server
type Server struct {
	input       io.Reader
	output      io.Writer
	tools       map[string]ToolHandler
	initialized bool
	clientInfo  ClientInfo
	mu          sync.RWMutex

	// Dependencies injected from main — typed store interfaces from store package
	SessionStore    store.SessionStore
	DecisionStore   store.DecisionStore
	CommitStore     store.CommitStore
	EscalationStore store.EscalationStore
	DocStore        store.DocStore

	// HTTP sidecar port for git hook integration (set by main, returned in session results)
	HTTPPort int

	// Policy and context engines (kept as local interfaces)
	ContextEngine       ContextEngine
	PolicyEngine        PolicyEngine        // Legacy OPA-only engine
	UnifiedPolicyEngine UnifiedPolicyEngine // New unified policy engine
}

// ToolHandler is the function signature for tool implementations
type ToolHandler func(ctx context.Context, args json.RawMessage) (*CallToolResult, error)

// ContextEngine assembles context for agents (deferred to later phase).
type ContextEngine interface {
	AssembleContext(ctx context.Context, req interface{}) (interface{}, error)
}

// PolicyEngine is the legacy OPA-only engine (kept for backward compat).
type PolicyEngine interface {
	Evaluate(ctx context.Context, input interface{}, query string) (bool, error)
}

// UnifiedPolicyEngine interface for the new unified policy engine
type UnifiedPolicyEngine interface {
	Evaluate(ctx context.Context, input *UnifiedEvalInput) (*UnifiedEvalResult, error)
}

// UnifiedEvalInput for the unified policy engine
type UnifiedEvalInput struct {
	SessionID  string
	TrustLevel int
	Action     UnifiedActionInput
	Context    UnifiedContextInput
	Session    UnifiedSessionInput
}

// UnifiedActionInput describes the action being evaluated
type UnifiedActionInput struct {
	Type     string
	Target   UnifiedTargetInput
	Metadata map[string]interface{}
}

// UnifiedTargetInput describes the target of an action
type UnifiedTargetInput struct {
	Paths       []string
	Services    []string
	Environment string
}

// UnifiedContextInput provides environmental context
type UnifiedContextInput struct {
	Environment string
	Hour        int
}

// UnifiedSessionInput provides session context
type UnifiedSessionInput struct {
	TrustLevel int
}

// UnifiedEvalResult from the unified policy engine
type UnifiedEvalResult struct {
	Allowed          bool
	RequiresApproval bool
	DeniedReasons    []string
	MatchedPolicies  []string
	Warnings         []string
}

// NewServer creates a new MCP server using stdio
func NewServer() *Server {
	return &Server{
		input:  os.Stdin,
		output: os.Stdout,
		tools:  make(map[string]ToolHandler),
	}
}

// NewServerWithIO creates a new MCP server with custom I/O (for testing)
func NewServerWithIO(input io.Reader, output io.Writer) *Server {
	return &Server{
		input:  input,
		output: output,
		tools:  make(map[string]ToolHandler),
	}
}

// RegisterTool registers a tool handler
func (s *Server) RegisterTool(name string, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[name] = handler
}

// Start starts the MCP server and processes messages
func (s *Server) Start(ctx context.Context) error {
	log.Println("Starting MCP server...")

	// Register built-in tools
	s.registerBuiltinTools()

	scanner := bufio.NewScanner(s.input)
	// Increase buffer size for large messages
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var request JSONRPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			s.sendError(nil, ErrCodeParse, "Parse error", nil)
			continue
		}

		response := s.handleRequest(ctx, &request)
		if response != nil {
			s.sendResponse(response)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// handleRequest processes a single JSON-RPC request
func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		// Notification, no response needed
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return s.handlePing(req)
	case "shutdown":
		return s.handleShutdown(req)
	default:
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	var params InitializeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", nil)
		}
	}

	s.mu.Lock()
	s.initialized = true
	s.clientInfo = params.ClientInfo
	s.mu.Unlock()

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapability{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
			Logging: &LoggingCapability{},
		},
		ServerInfo: ServerInfo{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}

	resultJSON, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsList returns the list of available tools
func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := s.getToolDefinitions()
	result := ToolsListResult{Tools: tools}

	resultJSON, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleToolsCall executes a tool
func (s *Server) handleToolsCall(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, ErrCodeInvalidParams, "Invalid params", nil)
	}

	s.mu.RLock()
	handler, exists := s.tools[params.Name]
	s.mu.RUnlock()

	if !exists {
		return s.errorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("Tool not found: %s", params.Name), nil)
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		// Return error as tool result, not JSON-RPC error
		errorResult := &CallToolResult{
			Content: []ToolContent{{
				Type: "text",
				Text: fmt.Sprintf("Error: %v", err),
			}},
			IsError: true,
		}
		resultJSON, _ := json.Marshal(errorResult)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  resultJSON,
		}
	}

	resultJSON, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handlePing handles the ping request
func (s *Server) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	resultJSON, _ := json.Marshal(map[string]string{})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// handleShutdown handles the shutdown request
func (s *Server) handleShutdown(req *JSONRPCRequest) *JSONRPCResponse {
	resultJSON, _ := json.Marshal(map[string]string{})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
}

// sendResponse writes a JSON-RPC response to the output
func (s *Server) sendResponse(resp *JSONRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to marshal response: %v", err)
		return
	}
	fmt.Fprintln(s.output, string(data))
}

// sendError sends a JSON-RPC error response
func (s *Server) sendError(id interface{}, code int, message string, data json.RawMessage) {
	s.sendResponse(&JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	})
}

// errorResponse creates an error response
func (s *Server) errorResponse(id interface{}, code int, message string, data json.RawMessage) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// getToolDefinitions returns the tool definitions for tools/list
func (s *Server) getToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "adp_start_session",
			Description: "Start a new ADP session for an AI agent. Returns session_id, session_token, and http_port. IMPORTANT: Before running git commands, you MUST set these environment variables: ADP_SESSION_ID=<session_id> ADP_SESSION_TOKEN=<session_token> ADP_URL=http://localhost:<http_port>. Example: ADP_SESSION_ID=adp_123 ADP_SESSION_TOKEN=mcp_adp_123 ADP_URL=http://localhost:8081 git commit -m 'message'.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"organization_id": {"type": "string", "description": "Organization UUID"},
					"user_id": {"type": "string", "description": "User UUID"},
					"tool": {"type": "string", "description": "Agent tool name (e.g., 'claude_code', 'cursor')"},
					"trust_level": {"type": "integer", "minimum": 1, "maximum": 5, "description": "Trust level 1-5"},
					"capabilities": {"type": "array", "items": {"type": "string"}, "description": "Requested capabilities"},
					"service_scope": {"type": "array", "items": {"type": "string"}, "description": "Service UUIDs in scope"}
				},
				"required": ["organization_id", "user_id", "tool"]
			}`),
		},
		{
			Name:        "adp_end_session",
			Description: "End an active ADP session.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID to end"}
				},
				"required": ["session_id"]
			}`),
		},
		{
			Name:        "adp_heartbeat",
			Description: "Send a heartbeat to keep the session alive.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"}
				},
				"required": ["session_id"]
			}`),
		},
		{
			Name:        "adp_get_context",
			Description: "Get token-budgeted context for a task. Returns relevant code, documentation, and constraints.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"},
					"service_id": {"type": "string", "description": "Target service UUID"},
					"task": {"type": "string", "description": "Task description for context retrieval"},
					"token_budget": {
						"type": "object",
						"properties": {
							"essential": {"type": "integer", "default": 4000},
							"task_relevant": {"type": "integer", "default": 12000},
							"supporting": {"type": "integer", "default": 8000}
						}
					}
				},
				"required": ["session_id", "task"]
			}`),
		},
		{
			Name:        "adp_check_action",
			Description: "Check if an action is allowed by governance policies. Returns approval status and any restrictions.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"},
					"action_type": {"type": "string", "description": "Type of action (e.g., 'modify_file', 'execute_command', 'deploy')"},
					"target": {
						"type": "object",
						"properties": {
							"paths": {"type": "array", "items": {"type": "string"}},
							"service_id": {"type": "string"},
							"environment": {"type": "string"}
						}
					},
					"context": {
						"type": "object",
						"description": "Additional context for policy evaluation"
					}
				},
				"required": ["session_id", "action_type", "target"]
			}`),
		},
		{
			Name:        "adp_request_approval",
			Description: "Request human approval for an action that requires escalation.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"},
					"action": {"type": "string", "description": "Action description"},
					"action_type": {"type": "string", "description": "Type of action"},
					"target": {"type": "object", "description": "Action target details"},
					"reason": {"type": "string", "description": "Why approval is needed"},
					"priority": {"type": "string", "enum": ["low", "normal", "high", "critical"], "default": "normal"}
				},
				"required": ["session_id", "action", "reason"]
			}`),
		},
		{
			Name:        "adp_get_approval",
			Description: "Check the status of an approval request.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"approval_id": {"type": "string", "description": "Approval request UUID"}
				},
				"required": ["approval_id"]
			}`),
		},
		{
			Name:        "adp_log_decision",
			Description: "Log a decision made by the agent for audit trail.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"},
					"decision_type": {"type": "string", "description": "Type of decision (e.g., 'code_change', 'file_create', 'command_execute')"},
					"action": {"type": "string", "description": "Action taken"},
					"target": {"type": "object", "description": "Target of the action"},
					"reasoning": {"type": "object", "description": "Reasoning behind the decision"},
					"confidence": {"type": "number", "minimum": 0, "maximum": 1, "description": "Confidence score 0-1"},
					"alternatives": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"action": {"type": "string"},
								"reason": {"type": "string"},
								"confidence": {"type": "number"}
							}
						}
					}
				},
				"required": ["session_id", "decision_type", "action"]
			}`),
		},
		{
			Name:        "adp_prepare_commit",
			Description: "Register intent to commit changes. Call this BEFORE running git commit. The pre-commit hook will automatically validate with the ADP server. Returns a commit token and approval status.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"session_id": {"type": "string", "description": "Session ID"},
					"files": {"type": "array", "items": {"type": "string"}, "description": "Files being committed"},
					"message": {"type": "string", "description": "Commit message"}
				},
				"required": ["session_id", "files"]
			}`),
		},
		{
			Name:        "adp_verify_commit",
			Description: "Verify that a commit has a valid audit trail.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"commit_sha": {"type": "string", "description": "Git commit SHA to verify"}
				},
				"required": ["commit_sha"]
			}`),
		},
		{
			Name:        "adp_get_docs",
			Description: "Query ADP-generated documentation. Returns curated docs based on category, session, or search query.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"category": {"type": "string", "enum": ["session_summary", "risk_report", "pattern_report"], "description": "Filter by doc category"},
					"session_id": {"type": "string", "description": "Filter by session ID"},
					"query": {"type": "string", "description": "Search query for doc content"},
					"limit": {"type": "integer", "default": 10, "description": "Max results to return"}
				}
			}`),
		},
	}
}

// registerBuiltinTools registers the standard ADP tools
func (s *Server) registerBuiltinTools() {
	s.RegisterTool("adp_start_session", s.handleStartSession)
	s.RegisterTool("adp_end_session", s.handleEndSession)
	s.RegisterTool("adp_heartbeat", s.handleHeartbeat)
	s.RegisterTool("adp_get_context", s.handleGetContext)
	s.RegisterTool("adp_check_action", s.handleCheckAction)
	s.RegisterTool("adp_request_approval", s.handleRequestApproval)
	s.RegisterTool("adp_get_approval", s.handleGetApproval)
	s.RegisterTool("adp_log_decision", s.handleLogDecision)
	s.RegisterTool("adp_prepare_commit", s.handlePrepareCommit)
	s.RegisterTool("adp_verify_commit", s.handleVerifyCommit)
	s.RegisterTool("adp_get_docs", s.handleGetDocs)
}

// textResult creates a simple text result
func textResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: text,
		}},
	}
}

// jsonResult creates a JSON result
func jsonResult(v interface{}) *CallToolResult {
	data, _ := json.MarshalIndent(v, "", "  ")
	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(data),
		}},
	}
}
