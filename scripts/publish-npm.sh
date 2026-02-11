#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Publish all mcp-cron npm packages.
# Platform-specific packages are published first, then the main package.
#
# Usage:
#   ./scripts/publish-npm.sh [--dry-run]

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DRY_RUN=""

if [ "${1:-}" = "--dry-run" ]; then
  DRY_RUN="--dry-run"
  echo "Dry run mode enabled"
fi

PLATFORMS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

# Publish platform packages first
for platform in "${PLATFORMS[@]}"; do
  pkg_name="mcp-cron-${platform%/*}-${platform#*/}"
  echo "Publishing $pkg_name..."
  npm publish "$REPO_ROOT/npm/$pkg_name" --access public $DRY_RUN
done

# Publish main package last
echo "Publishing mcp-cron..."
npm publish "$REPO_ROOT/npm/mcp-cron" --access public $DRY_RUN

echo ""
echo "All packages published successfully."
