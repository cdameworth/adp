package governance

import (
	"context"
	"testing"
)

// Smoke tests for policies/default.rego: the OPA-evaluated policy must agree
// with the builtin sensitive-path enforcement (#15). The canonical pattern
// list lives in internal/sensitivepaths; default.rego mirrors it.

func evalDefaultRego(t *testing.T, actionType string, trust int, paths []string) bool {
	t.Helper()
	engine := NewOPAEngine("../../../policies/default.rego")
	input := map[string]interface{}{
		"action": map[string]interface{}{
			"type":   actionType,
			"target": map[string]interface{}{"paths": paths},
		},
		"session": map[string]interface{}{"trust_level": trust},
		"context": map[string]interface{}{},
	}
	allowed, err := engine.Evaluate(context.Background(), input, "data.adp.governance.allow")
	if err != nil {
		t.Fatalf("rego evaluation failed: %v", err)
	}
	return allowed
}

func TestDefaultRego_BlocksNestedDotenvVariant(t *testing.T) {
	// Regression for #15: ".env*" full-path matching missed this.
	if allowed := evalDefaultRego(t, "modify_file", 3, []string{"config/.env.local"}); allowed {
		t.Error("expected modify_file on config/.env.local to be denied")
	}
}

func TestDefaultRego_BlocksCredentialsAtDepth(t *testing.T) {
	if allowed := evalDefaultRego(t, "modify_file", 3, []string{"deploy/us-east/credentials.json"}); allowed {
		t.Error("expected modify_file on nested credentials.json to be denied")
	}
}

func TestDefaultRego_BlocksSecretsTree(t *testing.T) {
	if allowed := evalDefaultRego(t, "modify_file", 3, []string{"services/api/secrets/prod.yaml"}); allowed {
		t.Error("expected modify_file under secrets/ tree to be denied")
	}
}

func TestDefaultRego_AllowsSafeModifyAtTrust3(t *testing.T) {
	if allowed := evalDefaultRego(t, "modify_file", 3, []string{"internal/app/main.go"}); !allowed {
		t.Error("expected modify_file on safe path at trust 3 to be allowed")
	}
}

func TestDefaultRego_DeniesModifyBelowTrust3(t *testing.T) {
	if allowed := evalDefaultRego(t, "modify_file", 2, []string{"internal/app/main.go"}); allowed {
		t.Error("expected modify_file at trust 2 to be denied")
	}
}

func TestDefaultRego_ReadsAlwaysAllowed(t *testing.T) {
	if allowed := evalDefaultRego(t, "read", 1, []string{".env"}); !allowed {
		t.Error("expected read to be allowed (detection is enforcement's job, not the allow rule)")
	}
}
