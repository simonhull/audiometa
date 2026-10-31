#!/usr/bin/env bash
set -euo pipefail

echo "Running audiometa linters..."
echo

# Use the mise-pinned toolchain so local lint matches CI
if ! command -v mise &> /dev/null; then
    echo "Error: mise not found"
    echo "Install: https://mise.jdx.dev/getting-started.html, then run: mise install"
    exit 1
fi

echo "→ Running golangci-lint ($(mise exec -- golangci-lint version --short 2>/dev/null || echo pinned))..."
mise exec -- golangci-lint run ./...

# Run go vet
echo
echo "→ Running go vet..."
mise exec -- go vet ./...

# Check formatting
echo
echo "→ Checking formatting..."
UNFORMATTED=$(mise exec -- gofmt -l . | grep -v '^vendor/' || true)
if [ -n "$UNFORMATTED" ]; then
    echo "Error: The following files need formatting:"
    echo "$UNFORMATTED"
    echo
    echo "Run: gofmt -s -w ."
    exit 1
fi

echo
echo "✓ All linters passed!"
