package governance

import "testing"

func TestBuiltin_RequireBehavioralVerification(t *testing.T) {
	engine := NewUnifiedPolicyEngine(nil, "")
	eval := engine.builtinPolicies["require_behavioral_verification"]
	if eval == nil {
		t.Fatal("require_behavioral_verification not registered")
	}

	commitInput := func(sha string) *EvaluationInput {
		in := baseInput()
		in.Action.Type = "verify_commit"
		in.Action.Metadata = map[string]interface{}{}
		if sha != "" {
			in.Action.Metadata["commit_sha"] = sha
		}
		return in
	}

	t.Run("no checker installed allows (advisory no-op)", func(t *testing.T) {
		allowed, _ := eval(commitInput("abc123"), map[string]interface{}{})
		if !allowed {
			t.Error("expected allowed with no checker")
		}
	})

	t.Run("non-finalizing actions ignored", func(t *testing.T) {
		engine.SetBehavioralChecker(func(string) (bool, string) { return false, "nope" })
		in := commitInput("abc123")
		in.Action.Type = "modify_file"
		allowed, _ := eval(in, map[string]interface{}{})
		if !allowed {
			t.Error("expected allowed for modify_file")
		}
	})

	t.Run("missing sha allowed (gate enforces later)", func(t *testing.T) {
		engine.SetBehavioralChecker(func(string) (bool, string) { return false, "nope" })
		in := commitInput("")
		allowed, _ := eval(in, map[string]interface{}{})
		if !allowed {
			t.Error("expected allowed without head sha")
		}
	})

	t.Run("unattested commit denied with reason", func(t *testing.T) {
		engine.SetBehavioralChecker(func(string) (bool, string) {
			return false, "missing behavioral verification: no attested run"
		})
		allowed, reason := eval(commitInput("abc123"), map[string]interface{}{})
		if allowed || reason == "" {
			t.Errorf("expected denial with reason, got allowed=%v reason=%q", allowed, reason)
		}
	})

	t.Run("attested commit allowed", func(t *testing.T) {
		engine.SetBehavioralChecker(func(string) (bool, string) { return true, "" })
		allowed, _ := eval(commitInput("abc123"), map[string]interface{}{})
		if !allowed {
			t.Error("expected allowed with passing checker")
		}
	})
}
