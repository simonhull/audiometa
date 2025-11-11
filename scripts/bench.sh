#!/usr/bin/env bash
set -euo pipefail

echo "Running audiometa benchmarks..."
echo

# Run benchmarks
echo "→ Running benchmarks..."
go test -bench=. -benchmem ./...

# Run with profiling if requested
if [ "${PROFILE:-}" = "1" ]; then
    echo
    echo "→ Running benchmarks with CPU profiling..."
    go test -bench=BenchmarkOpen -cpuprofile=cpu.prof -memprofile=mem.prof

    echo
    echo "Profile data generated:"
    echo "  - cpu.prof (view with: go tool pprof cpu.prof)"
    echo "  - mem.prof (view with: go tool pprof mem.prof)"
    echo
    echo "To analyze CPU profile:"
    echo "  go tool pprof -http=:8080 cpu.prof"
fi

echo
echo "✓ Benchmarks complete!"
