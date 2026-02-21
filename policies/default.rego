package adp.governance

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

sensitive_paths := {".env*", "secrets/**", "*.key"}

touches_sensitive_paths {
    some path in input.action.target.paths
    some pattern in sensitive_paths
    glob.match(pattern, [], path)
}
