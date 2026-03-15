#!/usr/bin/env bash
set -euo pipefail

echo "=== gofmt check ==="
unformatted="$(gofmt -l . 2>&1 || true)"
if [[ -n "${unformatted}" ]]; then
  echo "ERROR: Files not formatted:"
  echo "${unformatted}"
  exit 1
fi
echo "  OK"

echo "=== go vet ==="
go vet ./...
echo "  OK"

echo "=== go test ==="
go test ./... -count=1
echo "  OK"

if [[ "$(go env GOOS)" != "windows" ]]; then
  echo "=== go test -race ==="
  go test -race ./... -count=1
  echo "  OK"
fi

echo "=== CLI smoke ==="
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT
binary="${tmpdir}/speedtest"
if [[ "$(go env GOOS)" == "windows" ]]; then
  binary="${binary}.exe"
fi
go build -o "${binary}" ./cmd/speedtest
"${binary}" --help >/dev/null
"${binary}" --version >/dev/null
echo "  OK"

echo ""
echo "All checks passed."
