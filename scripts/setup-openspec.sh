#!/bin/bash
# Setup OpenSpec tooling for local development
#
# This script initializes the OpenSpec workflow with configurations
# pointing to the tracked docs/ directory for specs and context.
#
# PREREQUISITES
# =============
# Node.js 20.19.0 or higher is required.
#
# INSTALLATION
# ============
# Install OpenSpec CLI via npm:
#
#   npm install -g @fission-ai/openspec@latest
#
# For more info: https://github.com/Fission-AI/OpenSpec/
#
# USAGE
# =====
#   ./scripts/setup-openspec.sh         # First-time setup
#   ./scripts/setup-openspec.sh update  # Regenerate skills after OpenSpec upgrade
#
# WHAT IT CREATES (all gitignored)
# ================================
# - openspec/config.yaml - OpenSpec configuration
# - .claude/commands/opsx/* - Claude Code commands
# - .claude/skills/openspec-*/* - Claude Code skills
# - Similar for .codex/, .gemini/, .github/ integrations

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_ROOT"

# Check for update mode
UPDATE_MODE=false
if [[ "$1" == "update" ]]; then
    UPDATE_MODE=true
fi

echo "Setting up OpenSpec tooling..."

# Create openspec directory structure
mkdir -p openspec

# Create or update config.yaml pointing to docs/
if [[ ! -f openspec/config.yaml ]] || [[ "$UPDATE_MODE" == "false" ]]; then
    cat > openspec/config.yaml << 'EOF'
schema: spec-driven

# Path to main specs (the source of truth)
specsPath: docs/specs

# Project context (optional)
# This is shown to AI when creating artifacts.
# Add your tech stack, conventions, style guides, domain knowledge, etc.
context: |
  Please refer to docs/context/language-features.md for language rules and the
  comprehensive checklist for implementing language changes.

# Per-artifact rules (optional)
# Add custom rules for specific artifacts.
# Example:
#   rules:
#     proposal:
#       - Keep proposals under 500 words
#       - Always include a "Non-goals" section
#     tasks:
#       - Break tasks into chunks of max 2 hours
EOF
    echo "Created openspec/config.yaml"
else
    echo "Keeping existing openspec/config.yaml"
fi

# Check if openspec CLI is available
if command -v openspec &> /dev/null; then
    if [[ "$UPDATE_MODE" == "true" ]]; then
        echo "Running 'openspec update' to regenerate AI agent skills..."
        openspec update
    else
        echo "Running 'openspec init' to generate AI agent skills..."
        # Use --tools to run non-interactively; config.yaml already exists
        openspec init --tools claude,codex,gemini,github-copilot
    fi
else
    echo ""
    echo "ERROR: 'openspec' CLI not found."
    echo ""
    echo "Install OpenSpec first (requires Node.js 20.19.0+):"
    echo "  npm install -g @fission-ai/openspec@latest"
    echo ""
    echo "For more info: https://github.com/Fission-AI/OpenSpec/"
    echo ""
    echo "Then re-run this script."
    exit 1
fi

echo ""
echo "Setup complete!"
echo ""
echo "Directory structure:"
echo "  docs/specs/          - Main specs (tracked, source of truth)"
echo "  docs/context/        - Shared context files (tracked)"
echo "  openspec/            - Workflow state and config (local only, gitignored)"
echo ""
echo "Quick start:"
echo "  /opsx:new        - Start a new change"
echo "  /opsx:continue   - Continue working on a change"
echo "  /opsx:apply      - Implement tasks from a change"
echo ""
echo "Run './scripts/setup-openspec.sh update' after upgrading OpenSpec."
