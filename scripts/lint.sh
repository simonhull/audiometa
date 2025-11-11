#!/usr/bin/env bash
set -euo pipefail

echo "Running audiometa linters..."
echo

# Check if golangci-lint is installed
if ! command -v golangci-lint &> /dev/null; then
    echo "Error: golangci-lint not found"
    echo "Install: https://golangci-lint.run/usage/install/"
    echo "Quick: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin"
    exit 1
fi

# Run golangci-lint
echo "→ Running golangci-lint..."
golangci-lint run ./...

# Run go vet
echo
echo "→ Running go vet..."
go vet ./...

# Check formatting
echo
echo "→ Checking formatting..."
UNFORMATTED=$(gofmt -l . | grep -v '^vendor/' || true)
if [ -n "$UNFORMATTED" ]; then
    echo "Error: The following files need formatting:"
    echo "$UNFORMATTED"
    echo
    echo "Run: gofmt -s -w ."
    exit 1
fi

echo
echo "✓ All linters passed!"
