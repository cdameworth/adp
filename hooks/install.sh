#!/bin/bash
# ADP Git Hooks Installer
# Installs ADP git hooks into the current repository.
#
# Usage: install.sh [options]
#   --force              Overwrite existing hooks
#   --uninstall          Remove ADP hooks
#   --check              Check if hooks are installed
#   --configure-bypass   Set up token-based bypass for human commits
#
# The installer creates wrapper hooks that call both ADP hooks and any
# existing hooks (renamed to <hook>.local).

set -e

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOKS=("pre-commit" "pre-push" "post-commit")

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_error() { echo -e "${RED}[ADP] ERROR: $1${NC}" >&2; }
log_warn() { echo -e "${YELLOW}[ADP] WARNING: $1${NC}" >&2; }
log_success() { echo -e "${GREEN}[ADP] $1${NC}"; }
log_info() { echo "[ADP] $1"; }

# Find git directory
find_git_dir() {
    local dir=$(git rev-parse --git-dir 2>/dev/null)
    if [ -z "$dir" ]; then
        log_error "Not a git repository"
        exit 1
    fi
    echo "$dir"
}

# Check if ADP hooks are installed
check_installed() {
    local git_dir=$(find_git_dir)
    local hooks_dir="$git_dir/hooks"
    local installed=true

    for hook in "${HOOKS[@]}"; do
        if [ -f "$hooks_dir/$hook" ]; then
            if grep -q "ADP_HOOK_WRAPPER" "$hooks_dir/$hook" 2>/dev/null; then
                log_info "$hook: installed"
            else
                log_warn "$hook: exists (not ADP)"
                installed=false
            fi
        else
            log_info "$hook: not installed"
            installed=false
        fi
    done

    if [ "$installed" = true ]; then
        return 0
    else
        return 1
    fi
}

# Install hooks
install_hooks() {
    local force=$1
    local git_dir=$(find_git_dir)
    local hooks_dir="$git_dir/hooks"

    # Create hooks directory if needed
    mkdir -p "$hooks_dir"

    log_info "Installing ADP git hooks..."

    for hook in "${HOOKS[@]}"; do
        local target="$hooks_dir/$hook"
        local source="$SCRIPT_DIR/$hook"
        local backup="$hooks_dir/$hook.local"

        # Check if source hook exists
        if [ ! -f "$source" ]; then
            log_warn "Source hook not found: $source"
            continue
        fi

        # Handle existing hooks
        if [ -f "$target" ]; then
            if grep -q "ADP_HOOK_WRAPPER" "$target" 2>/dev/null; then
                if [ "$force" = "true" ]; then
                    log_info "Updating $hook hook"
                else
                    log_info "$hook hook already installed"
                    continue
                fi
            else
                # Backup existing hook
                if [ -f "$backup" ] && [ "$force" != "true" ]; then
                    log_error "$hook.local already exists. Use --force to overwrite."
                    exit 1
                fi
                log_info "Backing up existing $hook to $hook.local"
                mv "$target" "$backup"
            fi
        fi

        # Create wrapper hook
        cat > "$target" << 'WRAPPER_EOF'
#!/bin/bash
# ADP_HOOK_WRAPPER - Do not remove this line
# ADP Git Hook Wrapper - Calls ADP hook and local hook if present

HOOK_NAME="$(basename "$0")"
HOOK_DIR="$(dirname "$0")"
EXIT_CODE=0

# Run ADP hook first
WRAPPER_EOF

        # Add the actual hook content
        echo "# ADP Hook Content" >> "$target"
        cat "$source" >> "$target"
        echo "" >> "$target"

        # Add local hook execution
        cat >> "$target" << 'WRAPPER_EOF'

# Run local hook if present
LOCAL_HOOK="$HOOK_DIR/$HOOK_NAME.local"
if [ -f "$LOCAL_HOOK" ] && [ -x "$LOCAL_HOOK" ]; then
    "$LOCAL_HOOK" "$@"
    local_exit=$?
    if [ $local_exit -ne 0 ] && [ $EXIT_CODE -eq 0 ]; then
        EXIT_CODE=$local_exit
    fi
fi

exit $EXIT_CODE
WRAPPER_EOF

        chmod +x "$target"
        log_success "Installed $hook hook"
    done

    log_success "ADP hooks installed successfully"
}

# Uninstall hooks
uninstall_hooks() {
    local git_dir=$(find_git_dir)
    local hooks_dir="$git_dir/hooks"

    log_info "Uninstalling ADP git hooks..."

    for hook in "${HOOKS[@]}"; do
        local target="$hooks_dir/$hook"
        local backup="$hooks_dir/$hook.local"

        if [ -f "$target" ]; then
            if grep -q "ADP_HOOK_WRAPPER" "$target" 2>/dev/null; then
                rm -f "$target"
                log_info "Removed $hook hook"

                # Restore local hook if present
                if [ -f "$backup" ]; then
                    mv "$backup" "$target"
                    log_info "Restored $hook.local as $hook"
                fi
            else
                log_warn "$hook is not an ADP hook, skipping"
            fi
        else
            log_info "$hook not installed"
        fi
    done

    log_success "ADP hooks uninstalled"
}

# Configure bypass token
configure_bypass() {
    local git_dir=$(find_git_dir)
    local adp_dir="$git_dir/adp"

    mkdir -p "$adp_dir"

    echo ""
    log_info "Configure ADP bypass token for human commits"
    log_info "This token allows humans to bypass ADP validation when committing."
    echo ""

    # Read token from stdin
    read -sp "[ADP] Enter bypass token: " TOKEN
    echo ""

    if [ -z "$TOKEN" ]; then
        log_error "Token cannot be empty"
        exit 1
    fi

    # Confirm
    read -sp "[ADP] Confirm bypass token: " TOKEN_CONFIRM
    echo ""

    if [ "$TOKEN" != "$TOKEN_CONFIRM" ]; then
        log_error "Tokens do not match"
        exit 1
    fi

    # Store SHA-256 hash of the token
    TOKEN_HASH=$(printf '%s' "$TOKEN" | shasum -a 256 | cut -d ' ' -f 1)
    echo "$TOKEN_HASH" > "$adp_dir/bypass_hash"
    chmod 600 "$adp_dir/bypass_hash"

    log_success "Bypass token configured"
    log_info "Hash stored at: $adp_dir/bypass_hash"
    echo ""
    log_info "Usage: ADP_BYPASS_TOKEN=<your-token> git commit -m 'message'"
    log_info "The token is verified against the stored hash at commit time."
}

# Parse arguments
FORCE=false
UNINSTALL=false
CHECK=false
CONFIGURE_BYPASS=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --force|-f)
            FORCE=true
            shift
            ;;
        --uninstall|-u)
            UNINSTALL=true
            shift
            ;;
        --check|-c)
            CHECK=true
            shift
            ;;
        --configure-bypass)
            CONFIGURE_BYPASS=true
            shift
            ;;
        --help|-h)
            echo "Usage: install.sh [options]"
            echo ""
            echo "Options:"
            echo "  --force, -f          Overwrite existing hooks"
            echo "  --uninstall, -u      Remove ADP hooks"
            echo "  --check, -c          Check installation status"
            echo "  --configure-bypass   Set up token-based bypass for human commits"
            echo "  --help, -h           Show this help message"
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Execute requested action
if [ "$CONFIGURE_BYPASS" = true ]; then
    configure_bypass
elif [ "$CHECK" = true ]; then
    check_installed
elif [ "$UNINSTALL" = true ]; then
    uninstall_hooks
else
    install_hooks "$FORCE"
fi
