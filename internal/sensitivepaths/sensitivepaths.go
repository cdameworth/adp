// Package sensitivepaths is the single source of truth for sensitive-file
// detection across ADP. The governance builtin policy, the git-hook HTTP
// sidecar, and the commit auto-approval handler all consume this package.
// policies/default.rego mirrors DefaultPatterns for OPA evaluation paths;
// keep the two in sync when adding patterns.
//
// Matching semantics:
//   - Paths are normalized before matching: backslashes become forward
//     slashes, leading "./" and "/" are stripped, and the path is cleaned
//     (collapsing ".." and duplicate separators).
//   - Patterns without a "/" match against the file's basename only, so
//     ".env*" blocks "deploy/us-east/.env.local" at any depth.
//   - Patterns containing "/" match against the full normalized path and
//     support "**" for directory trees (e.g. "**/secrets/**").
//   - Matching is case-insensitive (".ENV" and "Credentials.JSON" are
//     blocked) because agents choose casing and defenders should not lose
//     to it.
//
// Documented limitation: detection is path-based. A secret written to an
// innocuous filename, or reached through a symlink, is out of scope — pair
// with content scanning for that layer.
package sensitivepaths

import (
	"path"
	"strings"

	"github.com/gobwas/glob"
)

// DefaultPatterns is the canonical sensitive-path list. Basename patterns
// (no "/") match at any directory depth; patterns with "/" match the full
// normalized path.
var DefaultPatterns = []string{
	// Environment / dotenv files
	".env", ".env.*",
	// Private keys, certificates, keystores
	"*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks", "*.ppk",
	"id_rsa", "id_rsa.*", "id_ed25519", "id_ed25519.*",
	// Cloud / tooling credential files
	"credentials.json", "credentials.yaml", "credentials.yml",
	"*-credentials.json", "service-account*.json", "service_account*",
	".netrc", ".pgpass", "htpasswd", "*.kubeconfig", "*.secret", ".secrets",
	"secrets.yaml", "secrets.json", "private_key*",
	// Credential-bearing directory trees (full-path patterns)
	"secrets/**", "**/secrets/**",
	".ssh/**", "**/.ssh/**",
	".aws/**", "**/.aws/**",
	".kube/**", "**/.kube/**",
	".docker/**", "**/.docker/**",
	".env/**", "**/.env/**",
	"credentials/**", "**/credentials/**",
}

type compiled struct {
	raw      string
	g        glob.Glob
	fullPath bool
}

var defaultCompiled = compileAll(DefaultPatterns)

func compileAll(patterns []string) []compiled {
	out := make([]compiled, 0, len(patterns))
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		fullPath := strings.Contains(p, "/")
		var g glob.Glob
		var err error
		if fullPath {
			g, err = glob.Compile(p, '/')
		} else {
			g, err = glob.Compile(p)
		}
		if err != nil {
			// Invalid patterns are dropped rather than breaking enforcement;
			// the builtin tests guard DefaultPatterns validity.
			continue
		}
		out = append(out, compiled{raw: p, g: g, fullPath: fullPath})
	}
	return out
}

// Normalize canonicalizes an agent-supplied path for matching.
func Normalize(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimSpace(p)
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	p = path.Clean(p)
	if p == "." {
		return ""
	}
	p = strings.TrimPrefix(p, "/")
	return p
}

// Match reports whether path is sensitive under the default pattern set,
// returning the matched pattern when blocked.
func Match(p string) (bool, string) {
	return matchWith(defaultCompiled, p)
}

// MatchWith is Match against a caller-supplied pattern set (policy config
// override). The supplied set replaces the defaults entirely.
func MatchWith(patterns []string, p string) (bool, string) {
	return matchWith(compileAll(patterns), p)
}

// IsSensitive reports whether path is sensitive under the default set.
func IsSensitive(p string) bool {
	blocked, _ := Match(p)
	return blocked
}

func matchWith(patterns []compiled, p string) (bool, string) {
	normalized := strings.ToLower(Normalize(p))
	if normalized == "" {
		return false, ""
	}
	base := path.Base(normalized)
	for _, pat := range patterns {
		if pat.fullPath {
			if pat.g.Match(normalized) {
				return true, pat.raw
			}
			continue
		}
		if pat.g.Match(base) {
			return true, pat.raw
		}
	}
	return false, ""
}
