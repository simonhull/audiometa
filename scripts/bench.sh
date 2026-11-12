#!/usr/bin/env bash
set -euo pipefail

PROFILE_DIR="${PROFILE_DIR:-$(mktemp -d -t audiometa-prof.XXXXXX)}"

echo "Running audiometa benchmarks..."
echo

# Run benchmarks
echo "→ Running benchmarks..."
mise exec -- go test -bench=. -benchmem ./...

# Run with profiling if requested
if [ "${PROFILE:-}" = "1" ]; then
    echo
    echo "→ Running benchmarks with CPU profiling..."
    mise exec -- go test -run '^$' -bench=BenchmarkOpen -cpuprofile="$PROFILE_DIR/cpu.prof" -memprofile="$PROFILE_DIR/mem.prof" -o "$PROFILE_DIR/audiometa.test" .

    echo
    echo "Profile data generated:"
    echo "  - $PROFILE_DIR/cpu.prof (view with: go tool pprof $PROFILE_DIR/cpu.prof)"
    echo "  - $PROFILE_DIR/mem.prof (view with: go tool pprof $PROFILE_DIR/mem.prof)"
    echo
    echo "To analyze CPU profile:"
    echo "  go tool pprof -http=:8080 $PROFILE_DIR/cpu.prof"
fi

echo
echo "✓ Benchmarks complete!"
