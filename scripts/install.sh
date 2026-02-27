#!/usr/bin/env bash
set -euo pipefail

REPO="tsosunchia/iNetSpeed-CLI"
BINARY="speedtest"
RELEASE_BASE="https://github.com/${REPO}/releases/latest/download"
RELEASES_URL="https://github.com/${REPO}/releases/latest"

if [[ -t 1 && "${TERM:-}" != "dumb" ]]; then
  C_RESET=$'\033[0m'
  C_BLUE=$'\033[34m'
  C_YELLOW=$'\033[33m'
  C_RED=$'\033[31m'
  C_CYAN=$'\033[36m'
else
  C_RESET=''
  C_BLUE=''
  C_YELLOW=''
  C_RED=''
  C_CYAN=''
fi

log() {
  local en="$1"
  local zh="$2"
  printf '%b==>%b %s / %s\n' "$C_BLUE" "$C_RESET" "$en" "$zh"
}

warn() {
  local en="$1"
  local zh="$2"
  printf '%bWarning:%b %s / %s\n' "$C_YELLOW" "$C_RESET" "$en" "$zh" >&2
}

die() {
  local en="$1"
  local zh="$2"
  printf '%bError:%b %s / %s\n' "$C_RED" "$C_RESET" "$en" "$zh" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

apply_backspaces() {
  local input="$1"
  local output=""
  local i ch next

  for ((i = 0; i < ${#input}; i++)); do
    ch="${input:i:1}"
    if [[ "$ch" == "^" && $((i + 1)) -lt ${#input} ]]; then
      next="${input:i+1:1}"
      if [[ "$next" == "H" || "$next" == "?" ]]; then
        output="${output%?}"
        ((i++))
        continue
      fi
    fi
    case "$ch" in
      $'\b'|$'\177')
        output="${output%?}"
        ;;
      *)
        output+="$ch"
        ;;
    esac
  done

  printf '%s' "$output"
}

read_input() {
  local __var_name="$1"
  local prompt_en="$2"
  local prompt_zh="$3"
  local answer prompt
  local rl_ign_start=$'\001'
  local rl_ign_end=$'\002'

  if [[ -r /dev/tty ]]; then
    bind '"\C-h": backward-delete-char' >/dev/null 2>&1 || true
    bind '"\C-?": backward-delete-char' >/dev/null 2>&1 || true
    bind '"\e[3~": delete-char' >/dev/null 2>&1 || true

    printf '%b%s / %s%b\n' "$C_CYAN" "$prompt_en" "$prompt_zh" "$C_RESET" >&2
    if [[ -n "$C_CYAN" || -n "$C_RESET" ]]; then
      prompt="${rl_ign_start}${C_CYAN}${rl_ign_end}> ${rl_ign_start}${C_RESET}${rl_ign_end}"
    else
      prompt="> "
    fi

    if ! IFS= read -r -e -p "$prompt" answer < /dev/tty; then
      return 1
    fi
  else
    printf '%s / %s\n> ' "$prompt_en" "$prompt_zh" >&2
    IFS= read -r answer || return 1
  fi
  answer="$(apply_backspaces "$answer")"
  printf -v "$__var_name" '%s' "$answer"
  return 0
}

ask_yes_no() {
  local prompt_en="$1"
  local prompt_zh="$2"
  local default="${3:-n}"
  local prompt_suffix reply

  case "$default" in
    y|Y) prompt_suffix='[Y/n]' ;;
    n|N) prompt_suffix='[y/N]' ;;
    *) prompt_suffix='[y/n]' ;;
  esac

  while true; do
    if ! read_input reply "${prompt_en} ${prompt_suffix}" "${prompt_zh} ${prompt_suffix}"; then
      return 1
    fi

    reply="${reply#"${reply%%[![:space:]]*}"}"
    reply="${reply%"${reply##*[![:space:]]}"}"
    if [[ -z "$reply" ]]; then
      reply="$default"
    fi

    case "$reply" in
      y|Y|yes|YES|Yes|是|好|覆盖) return 0 ;;
      n|N|no|NO|No|否|不要|不覆盖) return 1 ;;
      *)
        warn "Please enter y or n." "请输入 y 或 n。"
        ;;
    esac
  done
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

  die "curl or wget is required." "需要安装 curl 或 wget。"
}

detect_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"

  case "$os" in
    Linux) os="linux" ;;
    Darwin)
      die "macOS is not supported by this installer. Please download from: ${RELEASES_URL}" "该安装脚本不支持 macOS，请从这里下载：${RELEASES_URL}"
      ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT*)
      die "Windows is not supported by this installer. Please download from: ${RELEASES_URL}" "该安装脚本不支持 Windows，请从这里下载：${RELEASES_URL}"
      ;;
    *)
      die "unsupported OS: ${os}. This installer only supports Linux. Binaries: ${RELEASES_URL}" "不支持的操作系统：${os}。该安装脚本仅支持 Linux。二进制下载：${RELEASES_URL}"
      ;;
  esac

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      die "unsupported architecture: ${arch}. Supported: amd64/arm64." "不支持的架构：${arch}。支持：amd64/arm64。"
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
  [[ -n "$expected" ]] || die "checksum for ${asset} not found in checksums-sha256.txt." "在 checksums-sha256.txt 中未找到 ${asset} 的校验值。"

  if has_cmd sha256sum; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif has_cmd shasum; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    die "sha256sum or shasum is required for checksum verification." "校验需要 sha256sum 或 shasum。"
  fi

  [[ "$actual" == "$expected" ]] || die "checksum mismatch for ${asset}." "${asset} 的校验失败。"
}

main() {
  local platform os arch asset tmp_dir bin_path sum_path install_dir target trap_cmd run_hint new_install_dir
  platform="$(detect_platform)"
  os="${platform%/*}"
  arch="${platform#*/}"
  asset="${BINARY}-${os}-${arch}"

  tmp_dir="$(mktemp -d)"
  trap_cmd="$(printf 'rm -rf -- %q' "$tmp_dir")"
  trap "$trap_cmd" EXIT
  bin_path="${tmp_dir}/${asset}"
  sum_path="${tmp_dir}/checksums-sha256.txt"

  log "Downloading ${asset} from latest release" "正在从最新发布下载 ${asset}"
  download "${RELEASE_BASE}/${asset}" "$bin_path"

  log "Downloading checksums" "正在下载校验文件"
  download "${RELEASE_BASE}/checksums-sha256.txt" "$sum_path"

  log "Verifying checksum" "正在校验文件完整性"
  verify_checksum "$bin_path" "$sum_path" "$asset"

  install_dir="$(choose_install_dir)"
  install_dir="${install_dir%/}"
  if [[ -z "$install_dir" ]]; then
    die "install dir resolved to empty." "安装目录为空。"
  fi
  if ! path_has_dir "$install_dir"; then
    warn "install dir not in PATH: ${install_dir}" "安装目录不在 PATH 中：${install_dir}"
    warn "falling back to current directory: ${PWD}" "将回退到当前目录：${PWD}"
    install_dir="$PWD"
  fi

  mkdir -p "$install_dir"
  [[ -w "$install_dir" ]] || die "install dir is not writable: ${install_dir}" "安装目录不可写：${install_dir}"

  target="${install_dir}/${BINARY}"
  while [[ -e "$target" ]]; do
    warn "Target file already exists: ${target}" "目标文件已存在：${target}"
    if ask_yes_no "Overwrite existing file?" "是否覆盖安装？" "n"; then
      break
    fi

    if ask_yes_no "Install to another path?" "是否安装到其他路径？" "y"; then
      if ! read_input new_install_dir "Enter a new install directory:" "请输入新的安装目录："; then
        die "Failed to read input. Installation aborted." "无法读取输入，安装中止。"
      fi
      new_install_dir="${new_install_dir%/}"
      if [[ -z "$new_install_dir" ]]; then
        warn "Install directory cannot be empty. Please try again." "安装目录不能为空，请重试。"
        continue
      fi
      mkdir -p "$new_install_dir"
      if [[ ! -w "$new_install_dir" ]]; then
        warn "Directory is not writable: ${new_install_dir}" "目录不可写：${new_install_dir}"
        continue
      fi
      if ! path_has_dir "$new_install_dir"; then
        warn "New directory is not in PATH: ${new_install_dir}" "新目录不在 PATH 中：${new_install_dir}"
      fi
      install_dir="$new_install_dir"
      target="${install_dir}/${BINARY}"
      continue
    fi

    die "Installation cancelled by user." "用户取消安装。"
  done

  if has_cmd install; then
    install "$bin_path" "$target"
  else
    cp "$bin_path" "$target"
  fi
  chmod +x "$target"

  log "Installed to ${target}" "安装完成：${target}"
  if [[ "$install_dir" == "$PWD" ]]; then
    run_hint="./${BINARY}"
  elif path_has_dir "$install_dir"; then
    run_hint="${BINARY}"
  else
    run_hint="${target}"
  fi
  log "Run with: ${run_hint}" "运行命令：${run_hint}"

  log "Checking version output" "正在检查版本信息"
  if "$target" --version >/dev/null 2>&1; then
    "$target" --version
  else
    warn "Unable to detect version from installed binary." "无法从已安装二进制读取版本信息。"
  fi
}

main "$@"
