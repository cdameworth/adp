package governance

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock PolicyStore
// ---------------------------------------------------------------------------

type mockPolicyStore struct {
	policies []*PolicyDefinition
	err      error
	calls    int // tracks how many times ListEnabled was called
}

func (m *mockPolicyStore) ListEnabled(_ context.Context) ([]*PolicyDefinition, error) {
	m.calls++
	return m.policies, m.err
}

// ---------------------------------------------------------------------------
// Helper: build a minimal EvaluationInput
// ---------------------------------------------------------------------------

func baseInput() *EvaluationInput {
	return &EvaluationInput{
		SessionID:  "test-session",
		UserID:     "test-user",
		TrustLevel: 3,
		Action: ActionEvalInput{
			Type: "code_change",
			Target: TargetEvalInput{
				Paths:       []string{"main.go"},
				Services:    []string{"api"},
				Environment: "staging",
			},
			Metadata: map[string]interface{}{},
		},
		Context: ContextEvalInput{
			Environment: "staging",
			Time:        time.Now(),
			Hour:        10,
		},
		Session: SessionEvalInput{
			TrustLevel: 3,
		},
	}
}

// ---------------------------------------------------------------------------
// 1. Builtin Evaluator: deny_sensitive_files
// ---------------------------------------------------------------------------

func TestBuiltin_DenySensitiveFiles(t *testing.T) {
	t.Helper()
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["deny_sensitive_files"]
	if eval == nil {
		t.Fatal("deny_sensitive_files evaluator not registered")
	}

	tests := []struct {
		name    string
		paths   []string
		config  map[string]interface{}
		allowed bool
	}{
		{
			name:    "blocks .env file",
			paths:   []string{".env"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "blocks nested .env file",
			paths:   []string{"config/.env"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "pem glob catches server.pem",
			paths:   []string{"server.pem"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "pem glob catches nested cert.pem",
			paths:   []string{"certs/cert.pem"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "key glob catches private.key",
			paths:   []string{"private.key"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "key glob catches nested server.key",
			paths:   []string{"tls/server.key"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "blocks *.secret file via suffix pattern",
			paths:   []string{"database.secret"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "blocks credentials.* file via prefix pattern",
			paths:   []string{"credentials.json"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "allows safe Go file",
			paths:   []string{"safe.go"},
			config:  nil,
			allowed: true,
		},
		{
			name:    "allows safe README",
			paths:   []string{"README.md"},
			config:  nil,
			allowed: true,
		},
		{
			name:    "blocks first sensitive in mixed list",
			paths:   []string{"main.go", ".env", "readme.md"},
			config:  nil,
			allowed: false,
		},
		{
			name:    "allows empty paths",
			paths:   []string{},
			config:  nil,
			allowed: true,
		},
		{
			name:  "custom patterns override defaults",
			paths: []string{".env"}, // default pattern, but overridden
			config: map[string]interface{}{
				"patterns": []interface{}{"*.lock"},
			},
			allowed: true, // .env no longer matched because defaults replaced
		},
		{
			name:  "custom patterns block matching file",
			paths: []string{"package.lock"},
			config: map[string]interface{}{
				"patterns": []interface{}{"*.lock"},
			},
			allowed: false,
		},
		{
			// Demonstrate that custom glob patterns can fix the .pem gap
			name:  "custom *.pem pattern catches server.pem",
			paths: []string{"server.pem"},
			config: map[string]interface{}{
				"patterns": []interface{}{"*.pem", "*.key", ".env", "*.secret", "credentials.*"},
			},
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := baseInput()
			input.Action.Target.Paths = tt.paths
			cfg := tt.config
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			allowed, reason := eval(input, cfg)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", allowed, tt.allowed, reason)
			}
			if !allowed && reason == "" {
				t.Error("denied but reason is empty")
			}
			if allowed && reason != "" {
				t.Errorf("allowed but reason is non-empty: %s", reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Builtin Evaluator: blast_radius_limit
// ---------------------------------------------------------------------------

func TestBuiltin_BlastRadiusLimit(t *testing.T) {
	t.Helper()
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["blast_radius_limit"]
	if eval == nil {
		t.Fatal("blast_radius_limit evaluator not registered")
	}

	makePaths := func(n int) []string {
		paths := make([]string, n)
		for i := range paths {
			paths[i] = "file" + string(rune('a'+i%26)) + ".go"
		}
		return paths
	}

	tests := []struct {
		name       string
		pathCount  int
		trustLevel int
		config     map[string]interface{}
		allowed    bool
	}{
		{
			name:       "5 files within default limit",
			pathCount:  5,
			trustLevel: 3,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "10 files at exact default limit",
			pathCount:  10,
			trustLevel: 3,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "15 files exceeds default limit",
			pathCount:  15,
			trustLevel: 3,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "11 files just over default limit",
			pathCount:  11,
			trustLevel: 2,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "trust level 4 bypasses default override",
			pathCount:  15,
			trustLevel: 4,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust level 5 bypasses default override",
			pathCount:  20,
			trustLevel: 5,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust level 3 does not bypass default override",
			pathCount:  15,
			trustLevel: 3,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "custom max_files from config",
			pathCount:  6,
			trustLevel: 2,
			config:     map[string]interface{}{"max_files": float64(5)},
			allowed:    false,
		},
		{
			name:       "custom max_files allows within limit",
			pathCount:  5,
			trustLevel: 2,
			config:     map[string]interface{}{"max_files": float64(5)},
			allowed:    true,
		},
		{
			name:      "custom trust_level_override from config",
			pathCount: 20,
			// Trust level 2 would normally be denied, but config sets override to 2
			trustLevel: 2,
			config:     map[string]interface{}{"trust_level_override": float64(2)},
			allowed:    true,
		},
		{
			name:       "zero files always allowed",
			pathCount:  0,
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := baseInput()
			input.Action.Target.Paths = makePaths(tt.pathCount)
			input.TrustLevel = tt.trustLevel
			cfg := tt.config
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			allowed, reason := eval(input, cfg)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", allowed, tt.allowed, reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. Builtin Evaluator: off_hours_production
// ---------------------------------------------------------------------------

func TestBuiltin_OffHoursProduction(t *testing.T) {
	t.Helper()
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["off_hours_production"]
	if eval == nil {
		t.Fatal("off_hours_production evaluator not registered")
	}

	tests := []struct {
		name        string
		actionType  string
		environment string
		hour        int
		trustLevel  int
		config      map[string]interface{}
		allowed     bool
	}{
		{
			name:        "deploy to prod at hour 23 blocked",
			actionType:  "deploy",
			environment: "production",
			hour:        23,
			trustLevel:  3,
			config:      nil,
			allowed:     false,
		},
		{
			name:        "deploy to prod at hour 2 blocked (past midnight)",
			actionType:  "deploy",
			environment: "production",
			hour:        2,
			trustLevel:  3,
			config:      nil,
			allowed:     false,
		},
		{
			name:        "deploy to prod at hour 10 allowed",
			actionType:  "deploy",
			environment: "production",
			hour:        10,
			trustLevel:  3,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "deploy to prod at hour 14 allowed",
			actionType:  "deploy",
			environment: "production",
			hour:        14,
			trustLevel:  3,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "deploy to prod at boundary hour 22 blocked",
			actionType:  "deploy",
			environment: "production",
			hour:        22,
			trustLevel:  3,
			config:      nil,
			allowed:     false,
		},
		{
			name:        "deploy to prod at boundary hour 6 allowed",
			actionType:  "deploy",
			environment: "production",
			hour:        6,
			trustLevel:  3,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "non-deploy action always allowed",
			actionType:  "code_change",
			environment: "production",
			hour:        23,
			trustLevel:  1,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "deploy to staging always allowed",
			actionType:  "deploy",
			environment: "staging",
			hour:        23,
			trustLevel:  1,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "non-prod non-deploy allowed at off-hours",
			actionType:  "test",
			environment: "staging",
			hour:        1,
			trustLevel:  1,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "trust level 5 bypasses off-hours block",
			actionType:  "deploy",
			environment: "production",
			hour:        23,
			trustLevel:  5,
			config:      nil,
			allowed:     true,
		},
		{
			name:        "trust level 4 does not bypass default min_trust",
			actionType:  "deploy",
			environment: "production",
			hour:        23,
			trustLevel:  4,
			config:      nil,
			allowed:     false,
		},
		{
			name:        "custom min_trust_level from config",
			actionType:  "deploy",
			environment: "production",
			hour:        23,
			trustLevel:  3,
			config:      map[string]interface{}{"min_trust_level": float64(3)},
			allowed:     true,
		},
		{
			name:        "custom start_hour and end_hour",
			actionType:  "deploy",
			environment: "production",
			hour:        20,
			trustLevel:  3,
			config:      map[string]interface{}{"start_hour": float64(18), "end_hour": float64(8)},
			allowed:     false,
		},
		{
			name:        "custom hours allow when outside window",
			actionType:  "deploy",
			environment: "production",
			hour:        12,
			trustLevel:  3,
			config:      map[string]interface{}{"start_hour": float64(18), "end_hour": float64(8)},
			allowed:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := baseInput()
			input.Action.Type = tt.actionType
			input.Action.Target.Environment = tt.environment
			input.Context.Hour = tt.hour
			input.TrustLevel = tt.trustLevel
			cfg := tt.config
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			allowed, reason := eval(input, cfg)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", allowed, tt.allowed, reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Builtin Evaluator: cost_limit
// ---------------------------------------------------------------------------

func TestBuiltin_CostLimit(t *testing.T) {
	t.Helper()
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["cost_limit"]
	if eval == nil {
		t.Fatal("cost_limit evaluator not registered")
	}

	tests := []struct {
		name       string
		cost       float64
		trustLevel int
		config     map[string]interface{}
		allowed    bool
	}{
		{
			name:       "trust 1 cost $5 within $10 limit",
			cost:       5.0,
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 1 cost $10 at exact limit",
			cost:       10.0,
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 1 cost $15 exceeds $10 limit",
			cost:       15.0,
			trustLevel: 1,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "trust 2 cost $50 at exact limit",
			cost:       50.0,
			trustLevel: 2,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 2 cost $51 exceeds limit",
			cost:       51.0,
			trustLevel: 2,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "trust 3 cost $100 within $200 limit",
			cost:       100.0,
			trustLevel: 3,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 3 cost $300 exceeds $200 limit",
			cost:       300.0,
			trustLevel: 3,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "trust 4 cost $999 within $1000 limit",
			cost:       999.0,
			trustLevel: 4,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 4 cost $1001 exceeds $1000 limit",
			cost:       1001.0,
			trustLevel: 4,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "trust 5 cost $9999 within $10000 limit",
			cost:       9999.0,
			trustLevel: 5,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "trust 5 cost $10001 exceeds $10000 limit",
			cost:       10001.0,
			trustLevel: 5,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "zero cost always allowed",
			cost:       0.0,
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "negative cost treated as allowed",
			cost:       -5.0,
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "missing cost metadata allowed",
			cost:       -1, // sentinel: we will nil out metadata
			trustLevel: 1,
			config:     nil,
			allowed:    true,
		},
		{
			name:       "unknown trust level falls back to $10 default",
			cost:       15.0,
			trustLevel: 99,
			config:     nil,
			allowed:    false,
		},
		{
			name:       "custom limits_by_trust from config",
			cost:       500.0,
			trustLevel: 1,
			config: map[string]interface{}{
				"limits_by_trust": map[string]interface{}{
					"1": float64(1000),
				},
			},
			allowed: true,
		},
		{
			name:       "custom limits_by_trust denies over limit",
			cost:       1500.0,
			trustLevel: 1,
			config: map[string]interface{}{
				"limits_by_trust": map[string]interface{}{
					"1": float64(1000),
				},
			},
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := baseInput()
			input.TrustLevel = tt.trustLevel
			if tt.cost == -1 {
				// Test missing metadata
				input.Action.Metadata = nil
			} else {
				input.Action.Metadata = map[string]interface{}{
					"estimated_cost": tt.cost,
				}
			}
			cfg := tt.config
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			allowed, reason := eval(input, cfg)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", allowed, tt.allowed, reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Builtin Evaluator: require_migration_approval
// ---------------------------------------------------------------------------

func TestBuiltin_RequireMigrationApproval(t *testing.T) {
	t.Helper()
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["require_migration_approval"]
	if eval == nil {
		t.Fatal("require_migration_approval evaluator not registered")
	}

	tests := []struct {
		name       string
		actionType string
		config     map[string]interface{}
		allowed    bool
	}{
		{
			name:       "migrate_database blocked",
			actionType: "migrate_database",
			config:     nil,
			allowed:    false,
		},
		{
			name:       "alter_schema blocked",
			actionType: "alter_schema",
			config:     nil,
			allowed:    false,
		},
		{
			name:       "code_change allowed",
			actionType: "code_change",
			config:     nil,
			allowed:    true,
		},
		{
			name:       "deploy allowed",
			actionType: "deploy",
			config:     nil,
			allowed:    true,
		},
		{
			name:       "empty action type allowed",
			actionType: "",
			config:     nil,
			allowed:    true,
		},
		{
			name:       "custom action_types from config",
			actionType: "drop_table",
			config: map[string]interface{}{
				"action_types": []interface{}{"drop_table", "truncate_table"},
			},
			allowed: false,
		},
		{
			name:       "custom action_types allows migrate_database when overridden",
			actionType: "migrate_database",
			config: map[string]interface{}{
				"action_types": []interface{}{"drop_table"},
			},
			allowed: true, // default list replaced by custom
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := baseInput()
			input.Action.Type = tt.actionType
			cfg := tt.config
			if cfg == nil {
				cfg = map[string]interface{}{}
			}
			allowed, reason := eval(input, cfg)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v (reason: %s)", allowed, tt.allowed, reason)
			}
			if !allowed && reason == "" {
				t.Error("denied but reason is empty")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Builtin Evaluator: rate_limit_api (placeholder, always allows)
// ---------------------------------------------------------------------------

func TestBuiltin_RateLimitAPI(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["rate_limit_api"]
	if eval == nil {
		t.Fatal("rate_limit_api evaluator not registered")
	}

	input := baseInput()
	allowed, reason := eval(input, map[string]interface{}{})
	if !allowed {
		t.Errorf("rate_limit_api should always allow, got denied: %s", reason)
	}
	if reason != "" {
		t.Errorf("rate_limit_api should return empty reason, got: %s", reason)
	}
}

// ---------------------------------------------------------------------------
// 7. matchesPattern helper
// ---------------------------------------------------------------------------

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		// Suffix patterns (*.ext)
		{"database.secret", "*.secret", true},
		{"my.secret", "*.secret", true},
		{"secret", "*.secret", false},
		{"secret.txt", "*.secret", false},

		// Prefix patterns (credentials.*)
		{"credentials.json", "credentials.*", true},
		{"credentials.yaml", "credentials.*", true},
		{"not_credentials.json", "credentials.*", false},

		// Exact match via filepath.Match on basename
		{".env", ".env", true},
		{"config/.env", ".env", true},
		// ".pem" and ".key" are exact-match patterns, NOT suffix globs
		{"server.pem", ".pem", false},
		{"private.key", ".key", false},
		{".pem", ".pem", true}, // exact basename match works
		{".key", ".key", true}, // exact basename match works

		// Wildcard patterns for full suffix matching
		{"server.pem", "*.pem", true},
		{"private.key", "*.key", true},
	}

	for _, tt := range tests {
		name := tt.path + " ~ " + tt.pattern
		t.Run(name, func(t *testing.T) {
			got := matchesPattern(tt.path, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. Engine: nil PolicyStore
// ---------------------------------------------------------------------------

func TestEngine_NilPolicyStore(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	ctx := context.Background()
	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.Allowed {
		t.Errorf("expected allowed with nil store, got denied: %v", result.DeniedReasons)
	}
	if result.RequiresApproval {
		t.Error("expected no approval required with nil store")
	}
	if len(result.MatchedPolicies) != 0 {
		t.Errorf("expected no matched policies, got %v", result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 9. Engine: empty policy list
// ---------------------------------------------------------------------------

func TestEngine_EmptyPolicies(t *testing.T) {
	store := &mockPolicyStore{policies: []*PolicyDefinition{}}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed with empty policies, got denied: %v", result.DeniedReasons)
	}
}

// ---------------------------------------------------------------------------
// 10. Engine: PolicyStore error adds warning but continues
// ---------------------------------------------------------------------------

func TestEngine_PolicyStoreError(t *testing.T) {
	store := &mockPolicyStore{
		policies: nil,
		err:      errors.New("database connection failed"),
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate should not return error on store failure, got: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed when store fails (graceful degradation)")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about failed policy load")
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if w == "failed to load database policies: database connection failed" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Errorf("expected specific warning message, got: %v", result.Warnings)
	}
}

// ---------------------------------------------------------------------------
// 11. Engine: builtin policy denies correctly
// ---------------------------------------------------------------------------

func TestEngine_BuiltinPolicyDenies(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "sensitive-files",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.Action.Target.Paths = []string{".env"}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected denial for .env file")
	}
	if !result.RequiresApproval {
		t.Error("expected RequiresApproval when denied")
	}
	if len(result.DeniedReasons) == 0 {
		t.Error("expected at least one denied reason")
	}
	if len(result.MatchedPolicies) == 0 {
		t.Error("expected at least one matched policy")
	}

	foundPolicy := false
	for _, p := range result.MatchedPolicies {
		if p == "sensitive-files" {
			foundPolicy = true
			break
		}
	}
	if !foundPolicy {
		t.Errorf("expected 'sensitive-files' in matched policies, got: %v", result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 12. Engine: builtin policy allows correctly
// ---------------------------------------------------------------------------

func TestEngine_BuiltinPolicyAllows(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "sensitive-files",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.Action.Target.Paths = []string{"main.go", "utils.go"}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed for safe files, got denied: %v", result.DeniedReasons)
	}
	if result.RequiresApproval {
		t.Error("expected no approval required when allowed")
	}
	// Policy was still matched/evaluated, just didn't deny
	if len(result.MatchedPolicies) == 0 {
		t.Error("expected policy to be matched even when allowing")
	}
}

// ---------------------------------------------------------------------------
// 13. Engine: policy MinTrustLevel skips low-trust inputs
// ---------------------------------------------------------------------------

func TestEngine_MinTrustLevelSkipsPolicy(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "high-trust-only",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 5, // Only applies to trust level 5+
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	// Trust level 3 should skip the policy entirely
	input := baseInput()
	input.TrustLevel = 3
	input.Action.Target.Paths = []string{".env"} // Would be denied if policy applied

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed because trust level 3 < MinTrustLevel 5, policy should be skipped")
	}
	if len(result.MatchedPolicies) != 0 {
		t.Errorf("expected no matched policies when trust level too low, got: %v", result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 14. Engine: policy MinTrustLevel applies at matching trust
// ---------------------------------------------------------------------------

func TestEngine_MinTrustLevelAppliesAtMatch(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "trust-3-policy",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 3,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.TrustLevel = 3
	input.Action.Target.Paths = []string{".env"}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected denial because trust level 3 >= MinTrustLevel 3, policy should apply")
	}
}

// ---------------------------------------------------------------------------
// 15. Engine: disabled policy is skipped
// ---------------------------------------------------------------------------

func TestEngine_DisabledPolicySkipped(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "disabled-policy",
				Enabled:       false,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.Action.Target.Paths = []string{".env"}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed because policy is disabled")
	}
	if len(result.MatchedPolicies) != 0 {
		t.Errorf("expected no matched policies for disabled policy, got: %v", result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 16. Engine: unknown builtin name allows by default
// ---------------------------------------------------------------------------

func TestEngine_UnknownBuiltinAllows(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "unknown-builtin",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "nonexistent_evaluator",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed for unknown builtin name")
	}
	// The policy is still matched (it was processed)
	if len(result.MatchedPolicies) == 0 {
		t.Error("expected unknown builtin to still appear in matched policies")
	}
}

// ---------------------------------------------------------------------------
// 17. Engine: custom policy type is skipped
// ---------------------------------------------------------------------------

func TestEngine_CustomPolicyTypeSkipped(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "custom-webhook",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "custom",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed for custom policy type (skipped)")
	}
	// Custom policies hit continue before appending to MatchedPolicies
	if len(result.MatchedPolicies) != 0 {
		t.Errorf("expected no matched policies for custom type (continue before append), got: %v", result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 18. Engine: multiple policies accumulate results
// ---------------------------------------------------------------------------

func TestEngine_MultiplePoliciesAccumulate(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "sensitive-files",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "deny_sensitive_files",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
			{
				ID:            "pol-2",
				Name:          "migration-approval",
				Enabled:       true,
				Priority:      2,
				PolicyType:    "builtin",
				BuiltinName:   "require_migration_approval",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	// This input triggers BOTH denials: sensitive file + migration action
	input := baseInput()
	input.Action.Type = "migrate_database"
	input.Action.Target.Paths = []string{".env"}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected denial when multiple policies deny")
	}
	if len(result.DeniedReasons) < 2 {
		t.Errorf("expected at least 2 denied reasons, got %d: %v", len(result.DeniedReasons), result.DeniedReasons)
	}
	if len(result.MatchedPolicies) < 2 {
		t.Errorf("expected at least 2 matched policies, got %d: %v", len(result.MatchedPolicies), result.MatchedPolicies)
	}
	if !result.RequiresApproval {
		t.Error("expected RequiresApproval when denied")
	}
}

// ---------------------------------------------------------------------------
// 19. Engine: mixed allow/deny across multiple policies
// ---------------------------------------------------------------------------

func TestEngine_MixedAllowDeny(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "rate-limit",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "rate_limit_api", // always allows
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
			{
				ID:            "pol-2",
				Name:          "migration-approval",
				Enabled:       true,
				Priority:      2,
				PolicyType:    "builtin",
				BuiltinName:   "require_migration_approval",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.Action.Type = "migrate_database"

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	// One policy allows, one denies. Overall should be denied.
	if result.Allowed {
		t.Error("expected denial when any policy denies")
	}
	if len(result.DeniedReasons) != 1 {
		t.Errorf("expected 1 denied reason, got %d: %v", len(result.DeniedReasons), result.DeniedReasons)
	}
	if len(result.MatchedPolicies) != 2 {
		t.Errorf("expected 2 matched policies, got %d: %v", len(result.MatchedPolicies), result.MatchedPolicies)
	}
}

// ---------------------------------------------------------------------------
// 20. Cache: InvalidateCache forces reload
// ---------------------------------------------------------------------------

func TestEngine_InvalidateCache(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "rate-limit",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "rate_limit_api",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	// First call loads and caches
	_, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("first Evaluate returned error: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("expected 1 store call after first evaluate, got %d", store.calls)
	}

	// Second call should use cache
	_, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("second Evaluate returned error: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("expected 1 store call (cached), got %d", store.calls)
	}

	// Invalidate cache
	engine.InvalidateCache()

	// Third call should reload from store
	_, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("third Evaluate returned error: %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("expected 2 store calls after invalidation, got %d", store.calls)
	}
}

// ---------------------------------------------------------------------------
// 21. Cache: policies cached within 5-minute window
// ---------------------------------------------------------------------------

func TestEngine_CacheWithinWindow(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	// Run multiple evaluations in rapid succession
	for i := 0; i < 5; i++ {
		_, err := engine.Evaluate(ctx, input)
		if err != nil {
			t.Fatalf("Evaluate %d returned error: %v", i, err)
		}
	}

	// Only 1 store call should have been made (all within cache window)
	if store.calls != 1 {
		t.Errorf("expected 1 store call across 5 evaluations (cached), got %d", store.calls)
	}
}

// ---------------------------------------------------------------------------
// 22. Cache: expired cache triggers reload
// ---------------------------------------------------------------------------

func TestEngine_CacheExpiry(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	// First call loads cache
	_, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("first Evaluate returned error: %v", err)
	}

	// Force cache to be expired by setting expiry in the past
	engine.mu.Lock()
	engine.cacheExpiry = time.Now().Add(-1 * time.Second)
	engine.mu.Unlock()

	// Next call should reload
	_, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("second Evaluate returned error: %v", err)
	}
	if store.calls != 2 {
		t.Errorf("expected 2 store calls after cache expiry, got %d", store.calls)
	}
}

// ---------------------------------------------------------------------------
// 23. Engine: RequiresApproval set only when denied
// ---------------------------------------------------------------------------

func TestEngine_RequiresApprovalOnlyWhenDenied(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "rate-limit",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "rate_limit_api",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.RequiresApproval {
		t.Error("RequiresApproval should be false when all policies allow")
	}
}

// ---------------------------------------------------------------------------
// 24. Engine: DeniedReasons and MatchedPolicies are initialized (not nil)
// ---------------------------------------------------------------------------

func TestEngine_ResultSlicesInitialized(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	ctx := context.Background()
	input := baseInput()

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.DeniedReasons == nil {
		t.Error("DeniedReasons should be initialized (empty slice), not nil")
	}
	if result.MatchedPolicies == nil {
		t.Error("MatchedPolicies should be initialized (empty slice), not nil")
	}
	if result.Warnings == nil {
		t.Error("Warnings should be initialized (empty slice), not nil")
	}
}

// ---------------------------------------------------------------------------
// 25. Engine: blast radius integration through full evaluation
// ---------------------------------------------------------------------------

func TestEngine_BlastRadiusIntegration(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "blast-radius",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "blast_radius_limit",
				Config:        map[string]interface{}{"max_files": float64(3)},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	// 2 files: allowed
	input := baseInput()
	input.Action.Target.Paths = []string{"a.go", "b.go"}
	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected 2 files allowed with max 3, got denied: %v", result.DeniedReasons)
	}

	// 5 files: denied
	input.Action.Target.Paths = []string{"a.go", "b.go", "c.go", "d.go", "e.go"}
	result, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected 5 files denied with max 3")
	}
	if !result.RequiresApproval {
		t.Error("expected RequiresApproval for blast radius denial")
	}
}

// ---------------------------------------------------------------------------
// 26. Engine: off-hours integration through full evaluation
// ---------------------------------------------------------------------------

func TestEngine_OffHoursIntegration(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "off-hours",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "off_hours_production",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	// Deploy to prod at hour 23: blocked
	input := baseInput()
	input.Action.Type = "deploy"
	input.Action.Target.Environment = "production"
	input.Context.Hour = 23
	input.TrustLevel = 3

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected off-hours deploy to prod to be denied")
	}

	// Deploy to prod at hour 10: allowed
	input.Context.Hour = 10
	engine.InvalidateCache()
	result, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected daytime deploy to prod to be allowed, got denied: %v", result.DeniedReasons)
	}
}

// ---------------------------------------------------------------------------
// 27. Engine: cost limit integration through full evaluation
// ---------------------------------------------------------------------------

func TestEngine_CostLimitIntegration(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "cost-limit",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "cost_limit",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	input := baseInput()
	input.TrustLevel = 2
	input.Action.Metadata = map[string]interface{}{
		"estimated_cost": float64(100.0),
	}

	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	// Trust 2 limit is $50, cost is $100 -> denied
	if result.Allowed {
		t.Error("expected $100 cost at trust level 2 to be denied (limit $50)")
	}
}

// ---------------------------------------------------------------------------
// 28. Constructor: all 6 builtins registered
// ---------------------------------------------------------------------------

func TestNewUnifiedPolicyEngine_BuiltinsRegistered(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")

	expectedBuiltins := []string{
		"deny_sensitive_files",
		"blast_radius_limit",
		"off_hours_production",
		"cost_limit",
		"require_migration_approval",
		"rate_limit_api",
	}

	for _, name := range expectedBuiltins {
		if _, exists := engine.builtinPolicies[name]; !exists {
			t.Errorf("builtin %q not registered", name)
		}
	}

	if len(engine.builtinPolicies) != len(expectedBuiltins) {
		t.Errorf("expected %d builtins, got %d", len(expectedBuiltins), len(engine.builtinPolicies))
	}
}

// ---------------------------------------------------------------------------
// 29. Constructor: cache duration set to 5 minutes
// ---------------------------------------------------------------------------

func TestNewUnifiedPolicyEngine_CacheDuration(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	expected := 5 * time.Minute
	if engine.cacheDuration != expected {
		t.Errorf("cacheDuration = %v, want %v", engine.cacheDuration, expected)
	}
}

// ---------------------------------------------------------------------------
// 30. Engine: evaluateBuiltin with unknown name returns allowed
// ---------------------------------------------------------------------------

func TestEvaluateBuiltin_UnknownName(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	policy := &PolicyDefinition{
		BuiltinName: "does_not_exist",
		Config:      map[string]interface{}{},
	}
	input := baseInput()

	allowed, reason := engine.evaluateBuiltin(policy, input)
	if !allowed {
		t.Error("expected unknown builtin to return allowed")
	}
	if reason != "" {
		t.Errorf("expected empty reason for unknown builtin, got: %s", reason)
	}
}

// ---------------------------------------------------------------------------
// 31. Engine: concurrent evaluations are safe
// ---------------------------------------------------------------------------

func TestEngine_ConcurrentEvaluations(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{
			{
				ID:            "pol-1",
				Name:          "rate-limit",
				Enabled:       true,
				Priority:      1,
				PolicyType:    "builtin",
				BuiltinName:   "rate_limit_api",
				Config:        map[string]interface{}{},
				MinTrustLevel: 0,
			},
		},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			input := baseInput()
			result, err := engine.Evaluate(ctx, input)
			if err != nil {
				done <- err
				return
			}
			if result == nil {
				done <- errors.New("result is nil")
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent evaluation %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 32. Engine: cache updated after store returns new policies
// ---------------------------------------------------------------------------

func TestEngine_CacheUpdatedWithNewPolicies(t *testing.T) {
	store := &mockPolicyStore{
		policies: []*PolicyDefinition{},
	}
	engine := NewUnifiedPolicyEngine(store, "")
	ctx := context.Background()
	input := baseInput()
	input.Action.Target.Paths = []string{".env"}

	// First evaluate with empty policies - allowed
	result, err := engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed with empty policy list")
	}

	// Update store and invalidate cache
	store.policies = []*PolicyDefinition{
		{
			ID:            "pol-1",
			Name:          "sensitive-files",
			Enabled:       true,
			Priority:      1,
			PolicyType:    "builtin",
			BuiltinName:   "deny_sensitive_files",
			Config:        map[string]interface{}{},
			MinTrustLevel: 0,
		},
	}
	engine.InvalidateCache()

	// Second evaluate should pick up new policy and deny
	result, err = engine.Evaluate(ctx, input)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Allowed {
		t.Error("expected denied after cache invalidation with new sensitive-files policy")
	}
}
