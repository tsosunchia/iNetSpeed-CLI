#!/usr/bin/env bash
set -euo pipefail

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

checksum_file="${DIST}/checksums-sha256.txt"
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

rm -rf "${DIST}"
mkdir -p "${DIST}"

checksum_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

echo "Building ${BINARY} ${VERSION} (${COMMIT}) ..."

for platform in "${PLATFORMS[@]}"; do
  goos="${platform%/*}"
  goarch="${platform#*/}"
  stage="${tmpdir}/${goos}-${goarch}"
  mkdir -p "${stage}"

  binary_name="${BINARY}"
  archive_name="${BINARY}-${goos}-${goarch}"
  if [[ "${goos}" == "windows" ]]; then
    binary_name="${binary_name}.exe"
  fi

  echo "  → ${goos}/${goarch}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${stage}/${binary_name}" ./cmd/speedtest

  cp README.md LICENSE "${stage}/"

  if [[ "${goos}" == "windows" ]]; then
    archive_path="${DIST}/${archive_name}.zip"
    (
      cd "${stage}"
      zip -q "${OLDPWD}/${archive_path}" "${binary_name}" README.md LICENSE
    )
  else
    archive_path="${DIST}/${archive_name}.tar.gz"
    tar -C "${stage}" -czf "${archive_path}" "${binary_name}" README.md LICENSE
  fi
done

: > "${checksum_file}"
for asset in "${DIST}"/*; do
  [[ "${asset}" == "${checksum_file}" ]] && continue
  printf "%s  %s\n" "$(checksum_cmd "${asset}")" "$(basename "${asset}")" >> "${checksum_file}"
done

echo "Done. Artifacts in ${DIST}/:"
ls -lh "${DIST}/"
