#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-tsosunchia/iNetSpeed-CLI}"
BINARY="${BINARY:-speedtest}"
RELEASE_BASE="${RELEASE_BASE:-https://github.com/${REPO}/releases/latest/download}"
RELEASES_URL="${RELEASES_URL:-https://github.com/${REPO}/releases/latest}"

if [[ -t 1 && "${TERM:-}" != "dumb" ]]; then
  C_RESET=$'\033[0m'
  C_BLUE=$'\033[34m'
  C_YELLOW=$'\033[33m'
  C_RED=$'\033[31m'
else
  C_RESET=''
  C_BLUE=''
  C_YELLOW=''
  C_RED=''
fi

log() {
  printf '%b==>%b %s\n' "$C_BLUE" "$C_RESET" "$1"
}

warn() {
  printf '%bWarning:%b %s\n' "$C_YELLOW" "$C_RESET" "$1" >&2
}

die() {
  printf '%bError:%b %s\n' "$C_RED" "$C_RESET" "$1" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

download() {
  local url="$1"
  local output="$2"
  if has_cmd curl; then
    curl -fsSL --retry 3 --connect-timeout 10 -o "$output" "$url"
    return
  fi
  if has_cmd wget; then
    wget -qO "$output" "$url"
    return
  fi
  die "curl or wget is required."
}

detect_platform() {
  local os arch ext
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Linux) os="linux"; ext="tar.gz" ;;
    Darwin) os="darwin"; ext="tar.gz" ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT*)
      die "Use scripts/install.ps1 on Windows: ${RELEASES_URL}"
      ;;
    *)
      die "unsupported OS: ${os}"
      ;;
  esac

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      die "unsupported architecture: ${arch}"
      ;;
  esac

  printf '%s/%s/%s\n' "$os" "$arch" "$ext"
}

choose_install_dir() {
  if [[ -n "${INSTALL_DIR:-}" ]]; then
    printf '%s\n' "${INSTALL_DIR}"
    return
  fi
  if [[ "$(id -u)" -eq 0 ]]; then
    printf '%s\n' "/usr/local/bin"
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

verify_checksum() {
  local file="$1"
  local checksum_file="$2"
  local asset="$3"
  local expected actual

  expected="$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$checksum_file")"
  [[ -n "$expected" ]] || die "checksum for ${asset} not found."

  if has_cmd sha256sum; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif has_cmd shasum; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    die "sha256sum or shasum is required."
  fi

  [[ "$actual" == "$expected" ]] || die "checksum mismatch for ${asset}"
}

path_has_dir() {
  local target="$1"
  local item
  IFS=':' read -r -a path_dirs <<< "${PATH:-}"
  for item in "${path_dirs[@]}"; do
    [[ "$item" == "$target" ]] && return 0
  done
  return 1
}

main() {
  local platform os arch ext asset archive_path sum_path tmpdir install_dir target extracted run_hint
  IFS='/' read -r os arch ext <<< "$(detect_platform)"
  asset="${BINARY}-${os}-${arch}.${ext}"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' EXIT
  archive_path="${tmpdir}/${asset}"
  sum_path="${tmpdir}/checksums-sha256.txt"

  log "Downloading ${asset}"
  download "${RELEASE_BASE}/${asset}" "${archive_path}"

  log "Downloading checksums-sha256.txt"
  download "${RELEASE_BASE}/checksums-sha256.txt" "${sum_path}"

  log "Verifying checksum"
  verify_checksum "${archive_path}" "${sum_path}" "${asset}"

  log "Extracting archive"
  tar -xzf "${archive_path}" -C "${tmpdir}"
  extracted="${tmpdir}/${BINARY}"

  install_dir="$(choose_install_dir)"
  install_dir="${install_dir%/}"
  mkdir -p "${install_dir}"
  [[ -w "${install_dir}" ]] || die "install dir is not writable: ${install_dir}"

  target="${install_dir}/${BINARY}"
  install "${extracted}" "${target}"
  chmod +x "${target}"

  log "Installed to ${target}"
  if ! path_has_dir "${install_dir}"; then
    warn "install dir is not in PATH: ${install_dir}"
  fi

  if [[ "${install_dir}" == "${PWD}" ]]; then
    run_hint="./${BINARY}"
  elif path_has_dir "${install_dir}"; then
    run_hint="${BINARY}"
  else
    run_hint="${target}"
  fi
  log "Run with: ${run_hint}"
  "${target}" --version || warn "version check failed"
}

main "$@"
