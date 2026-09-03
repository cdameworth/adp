package sensitivepaths

import "testing"

// mustBlock: paths that must always be denied, at any depth, in any case.
var mustBlock = []string{
	// dotenv family, including nested, prefixed, and case variants
	".env", ".env.local", ".env.production", "config/.env",
	"deploy/us-east/.env.local", "./.env", ".ENV", "Config/.Env.Local",
	// credential files
	"credentials.json", "deploy/credentials.json", "gcp-credentials.json",
	"service-account-prod.json", "service_account.json", "credentials.yaml", "credentials.yml",
	// keys, certs, keystores
	"certs/server.pem", "private.key", "tls/server.key", "id_rsa", "id_rsa.pub",
	"id_ed25519", "backup.p12", "store.pfx", "app.keystore", "trust.jks", "agent.ppk",
	// credential-bearing trees
	"secrets/app.yaml", "deploy/secrets/prod.yaml", ".secrets", "db.secret",
	".ssh/config", "home/user/.ssh/id_ed25519", ".aws/credentials", "root/.aws/config",
	".kube/config", "clusters/prod.kubeconfig", ".docker/config.json",
	// misc auth files
	".netrc", ".pgpass", "htpasswd",
	// legacy semantics preserved (previously enforced by substring matchers)
	".env/config", "src/credentials/file.go", "secrets.yaml", "secrets.json",
	"private_key", "keys/private_key_backup",
	// adversarial normalization
	`config\.env.local`, "../.env", "a/../.env", "//etc/.env",
}

// mustAllow: false-positive guards. If any of these start blocking, the
// pattern set has become unusable for real development.
var mustAllow = []string{
	"env.go", "environment.ts", "config/environment.yml", "src/env/parser.go",
	"docs/secrets-management.md", "docs/secrets.md",
	"keynote.md", "monkey.go", "src/keys/helper.go",
	"credentials.go", "credentials_test.go",
	"pubkey.pem.txt", "sample.env.example", "secret.md",
	"internal/sensitivepaths/sensitivepaths.go",
}

func TestMatch_MustBlock(t *testing.T) {
	for _, p := range mustBlock {
		if blocked, pat := Match(p); !blocked {
			t.Errorf("expected %q to be blocked, was allowed", p)
		} else if pat == "" {
			t.Errorf("expected matched pattern for %q", p)
		}
	}
}

func TestMatch_MustAllow(t *testing.T) {
	for _, p := range mustAllow {
		if blocked, pat := Match(p); blocked {
			t.Errorf("false positive: %q blocked by pattern %q", p, pat)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		`a\b\c`:          "a/b/c",
		"./.env":         ".env",
		"/.env":          ".env",
		"a//b/.env":      "a/b/.env",
		"a/../.env":      ".env",
		"":               "",
		"  spaced/.env ": "spaced/.env",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchWith_OverrideReplacesDefaults(t *testing.T) {
	custom := []string{"*.internal", "restricted/**"}

	if blocked, _ := MatchWith(custom, "x/y.internal"); !blocked {
		t.Error("custom pattern should block y.internal")
	}
	if blocked, _ := MatchWith(custom, "restricted/data.txt"); !blocked {
		t.Error("custom tree pattern should block restricted/")
	}
	// Defaults no longer apply when overridden
	if blocked, _ := MatchWith(custom, ".env"); blocked {
		t.Error("override set must replace defaults, not extend them")
	}
}

func TestDefaultPatterns_AllCompile(t *testing.T) {
	// compileAll drops invalid patterns silently; the default set must
	// survive compilation whole.
	if got := len(defaultCompiled); got != len(DefaultPatterns) {
		t.Fatalf("default pattern dropped during compile: %d compiled of %d", got, len(DefaultPatterns))
	}
}
