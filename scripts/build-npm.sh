#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Cross-compile mcp-cron for all supported platforms and place binaries
# into the npm package directories.
#
# Usage:
#   ./scripts/build-npm.sh [version]
#
# If version is provided, all package.json files will be updated to that version.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${1:-}"

PLATFORMS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "windows/arm64"
)

# Update version in all package.json files if specified
if [ -n "$VERSION" ]; then
  echo "Updating version to $VERSION in all package.json files..."
  for pkg_dir in "$REPO_ROOT"/npm/*/; do
    pkg_json="$pkg_dir/package.json"
    if [ -f "$pkg_json" ]; then
      # Update the version field
      tmp=$(mktemp)
      sed "s/\"version\": \"[^\"]*\"/\"version\": \"$VERSION\"/" "$pkg_json" > "$tmp"
      mv "$tmp" "$pkg_json"
    fi
  done

  # Update optionalDependencies versions in the main package
  main_pkg="$REPO_ROOT/npm/mcp-cron/package.json"
  for platform in "${PLATFORMS[@]}"; do
    pkg_name="mcp-cron-${platform%/*}-${platform#*/}"
    tmp=$(mktemp)
    sed "s/\"$pkg_name\": \"[^\"]*\"/\"$pkg_name\": \"$VERSION\"/" "$main_pkg" > "$tmp"
    mv "$tmp" "$main_pkg"
  done

  echo "All package.json files updated to version $VERSION"
fi

# Cross-compile for each platform
for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  pkg_name="mcp-cron-${GOOS}-${GOARCH}"
  bin_dir="$REPO_ROOT/npm/$pkg_name/bin"

  exe_name="mcp-cron"
  if [ "$GOOS" = "windows" ]; then
    exe_name="mcp-cron.exe"
  fi

  echo "Building $GOOS/$GOARCH -> npm/$pkg_name/bin/$exe_name"

  mkdir -p "$bin_dir"
  LDFLAGS=""
  if [ -n "$VERSION" ]; then
    LDFLAGS="-X github.com/jolks/mcp-cron/internal/config.Version=$VERSION"
  fi

  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -ldflags "$LDFLAGS" \
    -o "$bin_dir/$exe_name" \
    "$REPO_ROOT/cmd/mcp-cron/main.go"
done

# Copy README into main package
cp "$REPO_ROOT/README.md" "$REPO_ROOT/npm/mcp-cron/README.md"

# Generate README for each platform package
for platform in "${PLATFORMS[@]}"; do
  pkg_name="mcp-cron-${platform%/*}-${platform#*/}"
  echo "# $pkg_name" > "$REPO_ROOT/npm/$pkg_name/README.md"
  echo "" >> "$REPO_ROOT/npm/$pkg_name/README.md"
  echo "Platform-specific binary for [mcp-cron](https://www.npmjs.com/package/mcp-cron). Install \`mcp-cron\` instead — this package is installed automatically." >> "$REPO_ROOT/npm/$pkg_name/README.md"
done

echo ""
echo "Build complete. Binaries:"
find "$REPO_ROOT/npm" -name "mcp-cron" -o -name "mcp-cron.exe" | sort
