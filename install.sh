#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${1:-$HOME/.local/bin/schedule}"
mkdir -p "$(dirname "$OUT")"
cd "$ROOT"
go build -o "$OUT" .
echo "installed $OUT"
