package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear environment
	os.Unsetenv("ADP_URL")
	os.Unsetenv("ADP_SESSION_TOKEN")

	config := loadConfig()

	if config.URL != defaultURL {
		t.Errorf("expected URL '%s', got '%s'", defaultURL, config.URL)
	}
	if config.Token != "" {
		t.Errorf("expected empty token, got '%s'", config.Token)
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	os.Setenv("ADP_URL", "https://custom.example.com")
	os.Setenv("ADP_SESSION_TOKEN", "test-token-123")
	defer func() {
		os.Unsetenv("ADP_URL")
		os.Unsetenv("ADP_SESSION_TOKEN")
	}()

	config := loadConfig()

	if config.URL != "https://custom.example.com" {
		t.Errorf("expected custom URL, got '%s'", config.URL)
	}
	if config.Token != "test-token-123" {
		t.Errorf("expected token 'test-token-123', got '%s'", config.Token)
	}
}

func TestFindHooksDir_FromEnv(t *testing.T) {
	os.Setenv("ADP_HOOKS_DIR", "/custom/hooks/path")
	defer os.Unsetenv("ADP_HOOKS_DIR")

	dir := findHooksDir()
	if dir != "/custom/hooks/path" {
		t.Errorf("expected '/custom/hooks/path', got '%s'", dir)
	}
}

func TestAPIRequest_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got '%s'", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization 'Bearer test-token', got '%s'", r.Header.Get("Authorization"))
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Token:   "test-token",
		Timeout: defaultTimout,
	}

	resp, err := apiRequest(config, "GET", "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%v'", resp["status"])
	}
}

func TestAPIRequest_WithPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)

		if payload["test"] != "value" {
			t.Errorf("expected payload test='value', got '%v'", payload["test"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: defaultTimout,
	}

	resp, err := apiRequest(config, "POST", "/test", map[string]interface{}{"test": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp["id"] != "123" {
		t.Errorf("expected id '123', got '%v'", resp["id"])
	}
}

func TestAPIRequest_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: defaultTimout,
	}

	_, err := apiRequest(config, "GET", "/test", nil)
	if err == nil {
		t.Error("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to contain '400', got '%v'", err)
	}
}

func TestGetConfigPath(t *testing.T) {
	path := getConfigPath()

	if !strings.Contains(path, ".adp") {
		t.Errorf("expected path to contain '.adp', got '%s'", path)
	}
	if !strings.Contains(path, "config.json") {
		t.Errorf("expected path to contain 'config.json', got '%s'", path)
	}
}

func TestConfigInit(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "adp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Override config path
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	err = configInit()
	if err != nil {
		t.Fatalf("configInit failed: %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpDir, ".adp", "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if config["url"] != defaultURL {
		t.Errorf("expected url '%s', got '%v'", defaultURL, config["url"])
	}
}

func TestCreateHookWrapper(t *testing.T) {
	content := `#!/bin/bash
echo "Hello from hook"`

	wrapper := createHookWrapper("pre-commit", content)

	if !strings.Contains(wrapper, "ADP_HOOK_WRAPPER") {
		t.Error("wrapper should contain ADP_HOOK_WRAPPER marker")
	}
	if !strings.Contains(wrapper, "pre-commit") {
		t.Error("wrapper should contain hook name")
	}
	if !strings.Contains(wrapper, "Hello from hook") {
		t.Error("wrapper should contain original content")
	}
	if !strings.Contains(wrapper, "pre-commit.local") {
		t.Error("wrapper should reference local hook")
	}
}

func TestHandleSession_NoSubcommand(t *testing.T) {
	config := &Config{URL: "http://localhost:8080"}
	err := handleSession(config, []string{})

	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand required") {
		t.Errorf("expected 'subcommand required' error, got '%v'", err)
	}
}

func TestHandleHooks_NoSubcommand(t *testing.T) {
	err := handleHooks([]string{})

	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand required") {
		t.Errorf("expected 'subcommand required' error, got '%v'", err)
	}
}

func TestHandleCheck_NoAction(t *testing.T) {
	config := &Config{URL: "http://localhost:8080"}
	err := handleCheck(config, []string{})

	if err == nil {
		t.Error("expected error for missing action")
	}
	if !strings.Contains(err.Error(), "action type required") {
		t.Errorf("expected 'action type required' error, got '%v'", err)
	}
}

func TestHandleCheck_NoSession(t *testing.T) {
	os.Unsetenv("ADP_SESSION_ID")

	config := &Config{URL: "http://localhost:8080"}
	err := handleCheck(config, []string{"modify_file"})

	if err == nil {
		t.Error("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "no session ID") {
		t.Errorf("expected 'no session ID' error, got '%v'", err)
	}
}

func TestHandleApprove_NoRequestID(t *testing.T) {
	config := &Config{URL: "http://localhost:8080"}
	err := handleApprove(config, []string{})

	if err == nil {
		t.Error("expected error for missing request ID")
	}
	if !strings.Contains(err.Error(), "request ID") {
		t.Errorf("expected 'request ID' error, got '%v'", err)
	}
}

func TestHandleAudit_NoSubcommand(t *testing.T) {
	config := &Config{URL: "http://localhost:8080"}
	err := handleAudit(config, []string{})

	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand required") {
		t.Errorf("expected 'subcommand required' error, got '%v'", err)
	}
}

func TestHandleContext_NoSubcommand(t *testing.T) {
	config := &Config{URL: "http://localhost:8080"}
	err := handleContext(config, []string{})

	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand required") {
		t.Errorf("expected 'subcommand required' error, got '%v'", err)
	}
}

func TestContextGet_NoTask(t *testing.T) {
	os.Setenv("ADP_SESSION_ID", "test-session")
	defer os.Unsetenv("ADP_SESSION_ID")

	config := &Config{URL: "http://localhost:8080"}
	err := contextGet(config, []string{})

	if err == nil {
		t.Error("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "task description required") {
		t.Errorf("expected 'task description required' error, got '%v'", err)
	}
}

func TestHandleConfig_NoSubcommand(t *testing.T) {
	err := handleConfig([]string{})

	if err == nil {
		t.Error("expected error for missing subcommand")
	}
	if !strings.Contains(err.Error(), "subcommand required") {
		t.Errorf("expected 'subcommand required' error, got '%v'", err)
	}
}

func TestSessionStart_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions" || r.Method != "POST" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)

		if payload["trust_level"].(float64) != 3 {
			t.Errorf("expected trust_level 3, got %v", payload["trust_level"])
		}
		if payload["agent_tool"] != "claude_code" {
			t.Errorf("expected agent_tool 'claude_code', got %v", payload["agent_tool"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "session-123",
			"token": "token-abc",
		})
	}))
	defer server.Close()

	config := &Config{
		URL:     server.URL,
		Timeout: defaultTimout,
	}

	err := sessionStart(config, []string{"--trust-level", "3"})
	if err != nil {
		t.Fatalf("sessionStart failed: %v", err)
	}
}

func TestSessionStatus_NoSession(t *testing.T) {
	os.Unsetenv("ADP_SESSION_ID")

	config := &Config{URL: "http://localhost:8080"}
	err := sessionStatus(config, []string{})

	if err == nil {
		t.Error("expected error for missing session")
	}
}

func TestSessionEnd_NoSession(t *testing.T) {
	os.Unsetenv("ADP_SESSION_ID")

	config := &Config{URL: "http://localhost:8080"}
	err := sessionEnd(config, []string{})

	if err == nil {
		t.Error("expected error for missing session")
	}
}
