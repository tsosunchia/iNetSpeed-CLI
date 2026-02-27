#!/usr/bin/env bash
set -euo pipefail

echo "=== gofmt check ==="
UNFORMATTED=$(gofmt -l . 2>&1 || true)
if [[ -n "${UNFORMATTED}" ]]; then
  echo "ERROR: Files not formatted:"
  echo "${UNFORMATTED}"
  exit 1
fi
echo "  OK"

echo "=== go vet ==="
go vet ./...
echo "  OK"

echo "=== go test ==="
go test ./... -count=1
echo "  OK"

echo "=== go test -race ==="
go test -race ./... -count=1
echo "  OK"

echo "=== shell tests ==="
bash "$(dirname "$0")/apple-cdn-speedtest_test.sh"
echo "  OK"

echo ""
echo "All checks passed."
