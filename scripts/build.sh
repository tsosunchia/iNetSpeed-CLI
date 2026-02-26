#!/usr/bin/env bash
set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────
MODULE="github.com/tsosunchia/iNetSpeed-CLI"
BINARY="speedtest"
DIST="dist"

VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo "dev")}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w \
  -X main.version=${VERSION} \
  -X main.commit=${COMMIT} \
  -X main.date=${DATE}"

PLATFORMS=(
  "darwin/amd64"
  "darwin/arm64"
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
)

# ── Build ──────────────────────────────────────────────────────────────
rm -rf "${DIST}"
mkdir -p "${DIST}"

echo "Building ${BINARY} ${VERSION} (${COMMIT}) ..."

for platform in "${PLATFORMS[@]}"; do
  GOOS="${platform%/*}"
  GOARCH="${platform#*/}"
  output="${DIST}/${BINARY}-${GOOS}-${GOARCH}"
  if [[ "${GOOS}" == "windows" ]]; then
    output="${output}.exe"
  fi

  echo "  → ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${output}" ./cmd/speedtest/
done

# ── Checksums ──────────────────────────────────────────────────────────
echo "Generating checksums ..."
cd "${DIST}"
shasum -a 256 "${BINARY}"-* > checksums-sha256.txt
cd ..

echo "Done. Artifacts in ${DIST}/:"
ls -lh "${DIST}/"
