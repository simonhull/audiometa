#!/usr/bin/env bash
# Fuzz every Fuzz target in the module for FUZZTIME each (default 30s).
# Usage: scripts/fuzz.sh [fuzztime]
set -euo pipefail

fuzztime="${1:-30s}"
targets=$(go test -list '^Fuzz' ./... | awk '
	/^Fuzz/ { names[++n] = $1; next }
	/^ok/   { for (i = 1; i <= n; i++) print $2, names[i]; n = 0 }')
if [ -z "$targets" ]; then
	echo "no fuzz targets found" >&2
	exit 1
fi
while read -r pkg target; do
	echo "→ ${target} (${pkg}, ${fuzztime})"
	go test -run '^$' -fuzz "^${target}\$" -fuzztime "${fuzztime}" "${pkg}"
done <<< "${targets}"
