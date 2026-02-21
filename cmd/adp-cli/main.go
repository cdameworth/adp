// Package main provides the ADP command-line interface.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	version       = "1.0.0"
	defaultURL    = "http://localhost:8080"
	defaultTimout = 30 * time.Second
)

// Config holds the CLI configuration
type Config struct {
	URL     string
	Token   string
	Timeout time.Duration
}

func main() {
	config := loadConfig()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "version", "-v", "--version":
		fmt.Printf("ADP CLI v%s\n", version)
	case "help", "-h", "--help":
		if len(args) > 0 {
			printCommandHelp(args[0])
		} else {
			printUsage()
		}
	case "session":
		err = handleSession(config, args)
	case "hooks":
		err = handleHooks(args)
	case "check":
		err = handleCheck(config, args)
	case "approve":
		err = handleApprove(config, args)
	case "deny":
		err = handleDeny(config, args)
	case "audit":
		err = handleAudit(config, args)
	case "context":
		err = handleContext(config, args)
	case "config":
		err = handleConfig(args)
	case "health":
		err = handleHealth(config, args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig() *Config {
	config := &Config{
		URL:     defaultURL,
		Timeout: defaultTimout,
	}

	if url := os.Getenv("ADP_URL"); url != "" {
		config.URL = url
	}
	if token := os.Getenv("ADP_SESSION_TOKEN"); token != "" {
		config.Token = token
	}

	return config
}

func printUsage() {
	fmt.Printf(`ADP CLI v%s - Agent Developer Portal Command Line Interface

Usage: adp <command> [options]

Commands:
  session     Manage ADP sessions
  hooks       Manage git hooks
  check       Check if an action is allowed
  approve     Approve a pending request
  deny        Deny a pending request
  audit       View audit logs and decision lineage
  context     Get context for a task
  config      Manage CLI configuration
  health      Check server health
  version     Show version information
  help        Show help for a command

Environment Variables:
  ADP_URL           ADP server URL (default: http://localhost:8080)
  ADP_SESSION_ID    Current session ID
  ADP_SESSION_TOKEN Session authentication token
  ADP_BYPASS        Set to bypass ADP checks in git hooks

Examples:
  adp session start --service my-service
  adp hooks install
  adp check modify_file --target src/main.go
  adp audit list --session <session-id>

Use "adp help <command>" for more information about a command.
`, version)
}

func printCommandHelp(cmd string) {
	switch cmd {
	case "session":
		fmt.Print(`Session Management

Usage: adp session <subcommand> [options]

Subcommands:
  start       Start a new ADP session
  status      Show current session status
  end         End the current session
  list        List active sessions

Options for 'start':
  --service <name>       Service scope for the session
  --trust-level <1-5>    Trust level (default: 2)
  --tool <name>          Agent tool name (e.g., claude_code, cursor)
  --ttl <duration>       Session time-to-live (e.g., 8h)

Examples:
  adp session start --service my-api --trust-level 3
  adp session status
  adp session end
`)
	case "hooks":
		fmt.Print(`Git Hooks Management

Usage: adp hooks <subcommand> [options]

Subcommands:
  install     Install ADP git hooks
  uninstall   Remove ADP git hooks
  status      Check hook installation status

Options:
  --force     Overwrite existing hooks

The hooks enable ADP governance enforcement for git operations:
  - pre-commit: Validates session and registers commit intent
  - pre-push: Verifies audit trail for all commits
  - post-commit: Registers commit SHA with ADP

Examples:
  adp hooks install
  adp hooks install --force
  adp hooks uninstall
  adp hooks status
`)
	case "check":
		fmt.Print(`Action Check

Usage: adp check <action> [options]

Check if an action is allowed by ADP governance policies.

Options:
  --target <path>     Target file or resource
  --session <id>      Session ID (default: ADP_SESSION_ID)

Actions:
  modify_file     Modify a file
  create_file     Create a new file
  delete_file     Delete a file
  execute_command Execute a shell command
  deploy          Deploy to an environment

Examples:
  adp check modify_file --target src/main.go
  adp check deploy --target production
`)
	case "approve", "deny":
		fmt.Print(`Request Resolution

Usage: adp approve|deny <request-id> [options]
       adp approve commit --session <session-id>

Approve or deny a pending escalation request.

Options:
  --comment <text>   Add a comment to the resolution

Examples:
  adp approve abc123 --comment "Reviewed and approved"
  adp deny abc123 --comment "Needs more testing"
  adp approve commit --session sess_12345
`)
	case "audit":
		fmt.Print(`Audit Operations

Usage: adp audit <subcommand> [options]

Subcommands:
  list        List decision records
  show        Show a specific decision
  lineage     Show decision lineage chain

Options for 'list':
  --session <id>        Filter by session ID
  --since <time>        Filter by start time
  --until <time>        Filter by end time
  --limit <n>           Limit results (default: 50)

Options for 'lineage':
  --depth <n>           Maximum chain depth (default: 10)

Examples:
  adp audit list --session sess_12345
  adp audit show dec_abc123
  adp audit lineage dec_abc123 --depth 5
`)
	case "context":
		fmt.Print(`Context Operations

Usage: adp context <subcommand> [options]

Subcommands:
  get         Get context for a task
  search      Search for relevant context

Options for 'get':
  --task <description>  Task description
  --budget <tokens>     Total token budget (default: 24000)
  --service <id>        Service ID

Examples:
  adp context get --task "implement user authentication"
  adp context search "error handling patterns"
`)
	case "config":
		fmt.Print(`Configuration Management

Usage: adp config <subcommand> [options]

Subcommands:
  init        Initialize configuration file
  show        Show current configuration
  set         Set a configuration value
  get         Get a configuration value

Examples:
  adp config init
  adp config show
  adp config set url http://adp.example.com
  adp config get url
`)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
	}
}

// Session management

func handleSession(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand required: start, status, end, list")
	}

	switch args[0] {
	case "start":
		return sessionStart(config, args[1:])
	case "status":
		return sessionStatus(config, args[1:])
	case "end":
		return sessionEnd(config, args[1:])
	case "list":
		return sessionList(config, args[1:])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func sessionStart(config *Config, args []string) error {
	var service string
	var trustLevel int = 2
	var tool string = "claude_code"
	var ttl string = "8h"

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--service", "-s":
			if i+1 < len(args) {
				service = args[i+1]
				i++
			}
		case "--trust-level", "-t":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &trustLevel)
				i++
			}
		case "--tool":
			if i+1 < len(args) {
				tool = args[i+1]
				i++
			}
		case "--ttl":
			if i+1 < len(args) {
				ttl = args[i+1]
				i++
			}
		}
	}

	payload := map[string]interface{}{
		"trust_level": trustLevel,
		"agent_tool":  tool,
		"ttl":         ttl,
	}
	if service != "" {
		payload["service_scope"] = service
	}

	resp, err := apiRequest(config, "POST", "/v1/sessions", payload)
	if err != nil {
		return err
	}

	sessionID, _ := resp["id"].(string)
	token, _ := resp["token"].(string)

	fmt.Printf("Session started successfully\n")
	fmt.Printf("Session ID: %s\n", sessionID)
	fmt.Printf("\nTo use this session, set these environment variables:\n")
	fmt.Printf("  export ADP_SESSION_ID=%s\n", sessionID)
	if token != "" {
		fmt.Printf("  export ADP_SESSION_TOKEN=%s\n", token)
	}

	return nil
}

func sessionStatus(config *Config, args []string) error {
	sessionID := os.Getenv("ADP_SESSION_ID")

	for i := 0; i < len(args); i++ {
		if args[i] == "--session" || args[i] == "-s" {
			if i+1 < len(args) {
				sessionID = args[i+1]
			}
		}
	}

	if sessionID == "" {
		return fmt.Errorf("no session ID. Set ADP_SESSION_ID or use --session")
	}

	resp, err := apiRequest(config, "GET", "/v1/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

func sessionEnd(config *Config, args []string) error {
	sessionID := os.Getenv("ADP_SESSION_ID")

	for i := 0; i < len(args); i++ {
		if args[i] == "--session" || args[i] == "-s" {
			if i+1 < len(args) {
				sessionID = args[i+1]
			}
		}
	}

	if sessionID == "" {
		return fmt.Errorf("no session ID. Set ADP_SESSION_ID or use --session")
	}

	_, err := apiRequest(config, "DELETE", "/v1/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Session %s ended\n", sessionID)
	fmt.Printf("\nRemember to unset environment variables:\n")
	fmt.Printf("  unset ADP_SESSION_ID ADP_SESSION_TOKEN\n")

	return nil
}

func sessionList(config *Config, args []string) error {
	var status string
	var limit int = 50

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 < len(args) {
				status = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &limit)
				i++
			}
		}
	}

	path := fmt.Sprintf("/v1/sessions?limit=%d", limit)
	if status != "" {
		path += "&status=" + status
	}

	resp, err := apiRequest(config, "GET", path, nil)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

// Hooks management

func handleHooks(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand required: install, uninstall, status")
	}

	// Find the hooks directory (relative to the binary or from env)
	hooksDir := findHooksDir()

	switch args[0] {
	case "install":
		force := false
		for _, arg := range args[1:] {
			if arg == "--force" || arg == "-f" {
				force = true
			}
		}
		return hooksInstall(hooksDir, force)
	case "uninstall":
		return hooksUninstall(hooksDir)
	case "status":
		return hooksStatus()
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func findHooksDir() string {
	// Check environment variable first
	if dir := os.Getenv("ADP_HOOKS_DIR"); dir != "" {
		return dir
	}

	// Try to find relative to executable
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		// Check if we're in a development environment
		hooksPath := filepath.Join(exeDir, "..", "hooks")
		if _, err := os.Stat(hooksPath); err == nil {
			return hooksPath
		}
		// Check standard installation paths
		hooksPath = filepath.Join(exeDir, "hooks")
		if _, err := os.Stat(hooksPath); err == nil {
			return hooksPath
		}
	}

	// Default paths based on OS
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		return "/usr/local/share/adp/hooks"
	}
	return filepath.Join(os.Getenv("PROGRAMDATA"), "adp", "hooks")
}

func hooksInstall(hooksDir string, force bool) error {
	// Find git directory
	gitDir, err := findGitDir()
	if err != nil {
		return err
	}

	gitHooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(gitHooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hooks := []string{"pre-commit", "pre-push", "post-commit"}

	fmt.Println("Installing ADP git hooks...")

	for _, hook := range hooks {
		sourcePath := filepath.Join(hooksDir, hook)
		targetPath := filepath.Join(gitHooksDir, hook)
		backupPath := filepath.Join(gitHooksDir, hook+".local")

		// Check if source exists
		sourceContent, err := os.ReadFile(sourcePath)
		if err != nil {
			fmt.Printf("  %s: source not found, skipping\n", hook)
			continue
		}

		// Handle existing hook
		if _, err := os.Stat(targetPath); err == nil {
			existingContent, _ := os.ReadFile(targetPath)
			if strings.Contains(string(existingContent), "ADP_HOOK_WRAPPER") {
				if !force {
					fmt.Printf("  %s: already installed\n", hook)
					continue
				}
			} else {
				// Backup existing hook
				if _, err := os.Stat(backupPath); err == nil && !force {
					return fmt.Errorf("%s.local already exists. Use --force to overwrite", hook)
				}
				if err := os.Rename(targetPath, backupPath); err != nil {
					return fmt.Errorf("failed to backup %s: %w", hook, err)
				}
				fmt.Printf("  %s: backed up existing hook to %s.local\n", hook, hook)
			}
		}

		// Create wrapper hook
		wrapper := createHookWrapper(hook, string(sourceContent))
		if err := os.WriteFile(targetPath, []byte(wrapper), 0755); err != nil {
			return fmt.Errorf("failed to install %s: %w", hook, err)
		}

		fmt.Printf("  %s: installed\n", hook)
	}

	fmt.Println("ADP hooks installed successfully")
	return nil
}

func createHookWrapper(name, content string) string {
	return fmt.Sprintf(`#!/bin/bash
# ADP_HOOK_WRAPPER - Do not remove this line
# ADP Git Hook: %s

%s

# Run local hook if present
LOCAL_HOOK="$(dirname "$0")/%s.local"
if [ -f "$LOCAL_HOOK" ] && [ -x "$LOCAL_HOOK" ]; then
    "$LOCAL_HOOK" "$@"
fi
`, name, content, name)
}

func hooksUninstall(hooksDir string) error {
	gitDir, err := findGitDir()
	if err != nil {
		return err
	}

	gitHooksDir := filepath.Join(gitDir, "hooks")
	hooks := []string{"pre-commit", "pre-push", "post-commit"}

	fmt.Println("Uninstalling ADP git hooks...")

	for _, hook := range hooks {
		targetPath := filepath.Join(gitHooksDir, hook)
		backupPath := filepath.Join(gitHooksDir, hook+".local")

		content, err := os.ReadFile(targetPath)
		if err != nil {
			fmt.Printf("  %s: not installed\n", hook)
			continue
		}

		if !strings.Contains(string(content), "ADP_HOOK_WRAPPER") {
			fmt.Printf("  %s: not an ADP hook, skipping\n", hook)
			continue
		}

		if err := os.Remove(targetPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", hook, err)
		}

		// Restore backup if present
		if _, err := os.Stat(backupPath); err == nil {
			if err := os.Rename(backupPath, targetPath); err != nil {
				fmt.Printf("  %s: removed (failed to restore backup)\n", hook)
			} else {
				fmt.Printf("  %s: removed, restored %s.local\n", hook, hook)
			}
		} else {
			fmt.Printf("  %s: removed\n", hook)
		}
	}

	fmt.Println("ADP hooks uninstalled")
	return nil
}

func hooksStatus() error {
	gitDir, err := findGitDir()
	if err != nil {
		return err
	}

	gitHooksDir := filepath.Join(gitDir, "hooks")
	hooks := []string{"pre-commit", "pre-push", "post-commit"}

	fmt.Println("ADP Git Hooks Status:")

	allInstalled := true
	for _, hook := range hooks {
		targetPath := filepath.Join(gitHooksDir, hook)
		content, err := os.ReadFile(targetPath)

		var status string
		if err != nil {
			status = "not installed"
			allInstalled = false
		} else if strings.Contains(string(content), "ADP_HOOK_WRAPPER") {
			status = "installed (ADP)"
		} else {
			status = "exists (non-ADP)"
			allInstalled = false
		}

		fmt.Printf("  %s: %s\n", hook, status)
	}

	if allInstalled {
		fmt.Println("\nAll ADP hooks are installed.")
	} else {
		fmt.Println("\nRun 'adp hooks install' to install missing hooks.")
	}

	return nil
}

func findGitDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

// Action check

func handleCheck(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("action type required")
	}

	actionType := args[0]
	var target string
	sessionID := os.Getenv("ADP_SESSION_ID")

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--target", "-t":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		case "--session", "-s":
			if i+1 < len(args) {
				sessionID = args[i+1]
				i++
			}
		}
	}

	if sessionID == "" {
		return fmt.Errorf("no session ID. Set ADP_SESSION_ID or use --session")
	}

	payload := map[string]interface{}{
		"session_id":  sessionID,
		"action_type": actionType,
		"target":      map[string]interface{}{"path": target},
	}

	resp, err := apiRequest(config, "POST", "/v1/governance/check", payload)
	if err != nil {
		return err
	}

	allowed, _ := resp["allowed"].(bool)
	if allowed {
		fmt.Println("Action ALLOWED")
	} else {
		fmt.Println("Action DENIED")
		if reason, ok := resp["reason"].(string); ok {
			fmt.Printf("Reason: %s\n", reason)
		}
		if requiresApproval, ok := resp["requires_approval"].(bool); ok && requiresApproval {
			fmt.Println("\nThis action requires human approval.")
			fmt.Println("Request approval with: adp approve request --action", actionType)
		}
	}

	return nil
}

// Approval handling

func handleApprove(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("request ID or 'commit' required")
	}

	// Handle commit approval
	if args[0] == "commit" {
		return approveCommit(config, args[1:])
	}

	requestID := args[0]
	var comment string

	for i := 1; i < len(args); i++ {
		if args[i] == "--comment" || args[i] == "-c" {
			if i+1 < len(args) {
				comment = args[i+1]
				i++
			}
		}
	}

	payload := map[string]interface{}{
		"approved": true,
		"comment":  comment,
	}

	_, err := apiRequest(config, "PATCH", "/v1/governance/approvals/"+requestID, payload)
	if err != nil {
		return err
	}

	fmt.Printf("Request %s approved\n", requestID)
	return nil
}

func approveCommit(config *Config, args []string) error {
	sessionID := os.Getenv("ADP_SESSION_ID")

	for i := 0; i < len(args); i++ {
		if args[i] == "--session" || args[i] == "-s" {
			if i+1 < len(args) {
				sessionID = args[i+1]
			}
		}
	}

	if sessionID == "" {
		return fmt.Errorf("no session ID. Set ADP_SESSION_ID or use --session")
	}

	// Get pending commit for session and approve it
	resp, err := apiRequest(config, "GET", "/v1/governance/approvals/pending?session_id="+sessionID, nil)
	if err != nil {
		return err
	}

	// Find commit-related approval
	approvals, _ := resp["data"].([]interface{})
	if len(approvals) == 0 {
		fmt.Println("No pending approvals for this session")
		return nil
	}

	for _, a := range approvals {
		approval, _ := a.(map[string]interface{})
		if id, ok := approval["id"].(string); ok {
			_, err := apiRequest(config, "PATCH", "/v1/governance/approvals/"+id, map[string]interface{}{
				"approved": true,
				"comment":  "Approved via CLI",
			})
			if err != nil {
				return err
			}
			fmt.Printf("Approved: %s\n", id)
		}
	}

	return nil
}

func handleDeny(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("request ID required")
	}

	requestID := args[0]
	var comment string

	for i := 1; i < len(args); i++ {
		if args[i] == "--comment" || args[i] == "-c" {
			if i+1 < len(args) {
				comment = args[i+1]
				i++
			}
		}
	}

	payload := map[string]interface{}{
		"approved": false,
		"comment":  comment,
	}

	_, err := apiRequest(config, "PATCH", "/v1/governance/approvals/"+requestID, payload)
	if err != nil {
		return err
	}

	fmt.Printf("Request %s denied\n", requestID)
	return nil
}

// Audit operations

func handleAudit(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand required: list, show, lineage")
	}

	switch args[0] {
	case "list":
		return auditList(config, args[1:])
	case "show":
		return auditShow(config, args[1:])
	case "lineage":
		return auditLineage(config, args[1:])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func auditList(config *Config, args []string) error {
	var sessionID string
	var limit int = 50

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session", "-s":
			if i+1 < len(args) {
				sessionID = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &limit)
				i++
			}
		}
	}

	path := fmt.Sprintf("/v1/audit/decisions?limit=%d", limit)
	if sessionID != "" {
		path += "&session_id=" + sessionID
	}

	resp, err := apiRequest(config, "GET", path, nil)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

func auditShow(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("decision ID required")
	}

	resp, err := apiRequest(config, "GET", "/v1/audit/decisions/"+args[0], nil)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

func auditLineage(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("decision ID required")
	}

	decisionID := args[0]
	depth := 10

	for i := 1; i < len(args); i++ {
		if args[i] == "--depth" {
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &depth)
			}
		}
	}

	path := fmt.Sprintf("/v1/audit/decisions/%s/lineage?depth=%d", decisionID, depth)
	resp, err := apiRequest(config, "GET", path, nil)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

// Context operations

func handleContext(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand required: get, search")
	}

	switch args[0] {
	case "get":
		return contextGet(config, args[1:])
	case "search":
		return contextSearch(config, args[1:])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func contextGet(config *Config, args []string) error {
	sessionID := os.Getenv("ADP_SESSION_ID")
	var task string
	var budget int = 24000

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--task", "-t":
			if i+1 < len(args) {
				task = args[i+1]
				i++
			}
		case "--budget", "-b":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &budget)
				i++
			}
		case "--session", "-s":
			if i+1 < len(args) {
				sessionID = args[i+1]
				i++
			}
		}
	}

	if sessionID == "" {
		return fmt.Errorf("no session ID. Set ADP_SESSION_ID or use --session")
	}
	if task == "" {
		return fmt.Errorf("task description required (--task)")
	}

	payload := map[string]interface{}{
		"session_id": sessionID,
		"task":       task,
		"token_budget": map[string]int{
			"essential":     4000,
			"task_relevant": budget - 12000,
			"supporting":    8000,
		},
	}

	resp, err := apiRequest(config, "POST", "/v1/context", payload)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

func contextSearch(config *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("search query required")
	}

	query := strings.Join(args, " ")
	sessionID := os.Getenv("ADP_SESSION_ID")

	payload := map[string]interface{}{
		"session_id": sessionID,
		"task":       query,
	}

	resp, err := apiRequest(config, "POST", "/v1/context", payload)
	if err != nil {
		return err
	}

	printJSON(resp)
	return nil
}

// Config operations

func handleConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand required: init, show, set, get")
	}

	switch args[0] {
	case "init":
		return configInit()
	case "show":
		return configShow()
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: adp config set <key> <value>")
		}
		return configSet(args[1], args[2])
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: adp config get <key>")
		}
		return configGet(args[1])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".adp", "config.json")
}

func configInit() error {
	configPath := getConfigPath()
	configDir := filepath.Dir(configPath)

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	config := map[string]interface{}{
		"url":     defaultURL,
		"timeout": "30s",
	}

	data, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Configuration initialized at %s\n", configPath)
	return nil
}

func configShow() error {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Println("No configuration file found. Run 'adp config init' to create one.")
		fmt.Println("\nCurrent settings (from environment):")
		fmt.Printf("  ADP_URL: %s\n", os.Getenv("ADP_URL"))
		fmt.Printf("  ADP_SESSION_ID: %s\n", os.Getenv("ADP_SESSION_ID"))
		return nil
	}

	var config map[string]interface{}
	json.Unmarshal(data, &config)
	printJSON(config)
	return nil
}

func configSet(key, value string) error {
	configPath := getConfigPath()
	data, _ := os.ReadFile(configPath)

	var config map[string]interface{}
	if len(data) > 0 {
		json.Unmarshal(data, &config)
	} else {
		config = make(map[string]interface{})
	}

	config[key] = value

	newData, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(configPath, newData, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

func configGet(key string) error {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("no configuration file found")
	}

	var config map[string]interface{}
	json.Unmarshal(data, &config)

	if value, ok := config[key]; ok {
		fmt.Println(value)
	} else {
		return fmt.Errorf("key not found: %s", key)
	}

	return nil
}

// Health check

func handleHealth(config *Config, args []string) error {
	quiet := false
	for _, arg := range args {
		if arg == "--quiet" || arg == "-q" {
			quiet = true
		}
	}

	resp, err := apiRequest(config, "GET", "/health", nil)
	if err != nil {
		if !quiet {
			fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		}
		return err
	}

	status, _ := resp["status"].(string)
	if status == "ok" {
		if !quiet {
			fmt.Println("ADP server is healthy")
		}
		return nil
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "ADP server is unhealthy: %s\n", status)
	}
	return fmt.Errorf("unhealthy: %s", status)
}

// API helpers

func apiRequest(config *Config, method, path string, payload interface{}) (map[string]interface{}, error) {
	url := config.URL + path

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+config.Token)
	}

	client := &http.Client{Timeout: config.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respData))
	}

	var result map[string]interface{}
	if len(respData) > 0 {
		if err := json.Unmarshal(respData, &result); err != nil {
			// Return raw response if not JSON
			return map[string]interface{}{"raw": string(respData)}, nil
		}
	}

	return result, nil
}

func printJSON(data interface{}) {
	output, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(output))
}
