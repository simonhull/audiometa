#!/usr/bin/env bash
set -euo pipefail

echo "Running audiometa test suite..."
echo

# Run tests with coverage
echo "→ Running tests with coverage..."
go test -race -coverprofile=coverage.out ./...

# Generate coverage report
echo
echo "→ Generating coverage report..."
go tool cover -func=coverage.out | tail -1

# Optionally open HTML coverage report
if [ "${OPEN_COVERAGE:-}" = "1" ]; then
    echo "→ Opening HTML coverage report..."
    go tool cover -html=coverage.out -o coverage.html
    xdg-open coverage.html 2>/dev/null || open coverage.html 2>/dev/null || echo "coverage.html generated"
fi

echo
echo "✓ Tests complete!"
