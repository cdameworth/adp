package adp.governance

import future.keywords.in

default allow := false

# Read always allowed
allow {
    input.action.type == "read"
}
allow {
    input.action.type == "list"
}

# Modify requires trust level 3+
allow {
    input.action.type == "modify_file"
    input.session.trust_level >= 3
    not touches_sensitive_paths
}

# Production deploy requires level 5 or approval
requires_approval {
    input.action.type == "deploy"
    input.context.environment == "production"
    input.session.trust_level < 5
}

# Canonical list: internal/sensitivepaths (Go) is the single source of truth.
# The sets below mirror it for OPA evaluation paths — keep them in sync.
# Basename patterns match at any directory depth; tree patterns match the
# full normalized path. Matching is case-insensitive on the basename.

sensitive_name_patterns := {
    ".env", ".env.*",
    "*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks", "*.ppk",
    "id_rsa", "id_rsa.*", "id_ed25519", "id_ed25519.*",
    "credentials.json", "credentials.yaml", "credentials.yml", "*-credentials.json",
    "service-account*.json", "service_account*",
    ".netrc", ".pgpass", "htpasswd", "*.kubeconfig", "*.secret", ".secrets",
    "secrets.yaml", "secrets.json", "private_key*",
}

sensitive_tree_patterns := {
    "secrets/**", "**/secrets/**",
    ".ssh/**", "**/.ssh/**",
    ".aws/**", "**/.aws/**",
    ".kube/**", "**/.kube/**",
    ".docker/**", "**/.docker/**",
    ".env/**", "**/.env/**",
    "credentials/**", "**/credentials/**",
}

touches_sensitive_paths {
    some path in input.action.target.paths
    normalized := replace(path, "\\", "/")
    parts := split(normalized, "/")
    base := lower(parts[count(parts) - 1])
    some pattern in sensitive_name_patterns
    glob.match(pattern, [], base)
}

touches_sensitive_paths {
    some path in input.action.target.paths
    normalized := lower(replace(path, "\\", "/"))
    some pattern in sensitive_tree_patterns
    glob.match(pattern, ["/"], normalized)
}
