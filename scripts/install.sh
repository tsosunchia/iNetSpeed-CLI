#!/usr/bin/env bash
set -euo pipefail

REPO="tsosunchia/iNetSpeed-CLI"
BINARY="speedtest"
RELEASE_BASE="https://github.com/${REPO}/releases/latest/download"
RELEASES_URL="https://github.com/${REPO}/releases/latest"

log() {
  printf '==> %s\n' "$*"
}

warn() {
  printf 'Warning: %s\n' "$*" >&2
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

download() {
  local url="$1"
  local output="$2"

  if has_cmd curl; then
    curl -fL --retry 3 --connect-timeout 10 -o "$output" "$url"
    return
  fi

  if has_cmd wget; then
    wget -qO "$output" "$url"
    return
  fi

  die "curl or wget is required"
}

detect_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Linux) os="linux" ;;
    Darwin)
      die "macOS is not supported by this installer. Please download from: ${RELEASES_URL}"
      ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT*)
      die "Windows is not supported by this installer. Please download from: ${RELEASES_URL}"
      ;;
    *)
      die "unsupported OS: ${os}. This installer only supports Linux. Binaries: ${RELEASES_URL}"
      ;;
  esac

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      die "unsupported architecture: ${arch}. Supported: amd64/arm64."
      ;;
  esac

  printf '%s/%s\n' "$os" "$arch"
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

path_has_dir() {
  local target="$1"
  local item
  IFS=':' read -r -a path_dirs <<< "${PATH:-}"
  for item in "${path_dirs[@]}"; do
    [[ "$item" == "$target" ]] && return 0
  done
  return 1
}

verify_checksum() {
  local file="$1"
  local checksum_file="$2"
  local asset="$3"
  local expected actual

  expected="$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$checksum_file")"
  [[ -n "$expected" ]] || die "checksum for ${asset} not found in checksums-sha256.txt"

  if has_cmd sha256sum; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif has_cmd shasum; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    die "sha256sum or shasum is required for checksum verification"
  fi

  [[ "$actual" == "$expected" ]] || die "checksum mismatch for ${asset}"
}

main() {
  local platform os arch asset tmp_dir bin_path sum_path install_dir target trap_cmd run_hint
  platform="$(detect_platform)"
  os="${platform%/*}"
  arch="${platform#*/}"
  asset="${BINARY}-${os}-${arch}"

  tmp_dir="$(mktemp -d)"
  trap_cmd="$(printf 'rm -rf -- %q' "$tmp_dir")"
  trap "$trap_cmd" EXIT
  bin_path="${tmp_dir}/${asset}"
  sum_path="${tmp_dir}/checksums-sha256.txt"

  log "Downloading ${asset} from latest release"
  download "${RELEASE_BASE}/${asset}" "$bin_path"

  log "Downloading checksums"
  download "${RELEASE_BASE}/checksums-sha256.txt" "$sum_path"

  log "Verifying checksum"
  verify_checksum "$bin_path" "$sum_path" "$asset"

  install_dir="$(choose_install_dir)"
  install_dir="${install_dir%/}"
  if [[ -z "$install_dir" ]]; then
    die "install dir resolved to empty"
  fi
  if ! path_has_dir "$install_dir"; then
    warn "install dir not in PATH: ${install_dir}"
    warn "falling back to current directory: ${PWD}"
    install_dir="$PWD"
  fi

  mkdir -p "$install_dir"
  [[ -w "$install_dir" ]] || die "install dir is not writable: ${install_dir}"

  target="${install_dir}/${BINARY}"
  if has_cmd install; then
    install "$bin_path" "$target"
  else
    cp "$bin_path" "$target"
  fi
  chmod +x "$target"

  log "Installed to ${target}"
  if [[ "$install_dir" == "$PWD" ]]; then
    run_hint="./${BINARY}"
  else
    run_hint="${BINARY}"
  fi
  log "Run with: ${run_hint}"

  if "$target" --version >/dev/null 2>&1; then
    "$target" --version
  fi
}

main "$@"
