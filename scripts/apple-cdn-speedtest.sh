#!/bin/sh
set -f

# ============================================================
#  iNetSpeed-CLI  —  one-key script
#  Usage:  curl -sL <url> | bash
#
#  Env overrides:
#    DL_URL  UL_URL  LATENCY_URL  MAX  TIMEOUT  THREADS
# ============================================================

DL_URL="${DL_URL:-https://mensura.cdn-apple.com/api/v1/gm/large}"
UL_URL="${UL_URL:-https://mensura.cdn-apple.com/api/v1/gm/slurp}"
LATENCY_URL="${LATENCY_URL:-https://mensura.cdn-apple.com/api/v1/gm/small}"
MAX="${MAX:-2G}"
SIZE="${SIZE:-$MAX}"
TIMEOUT="${TIMEOUT:-10}"
THREADS="${THREADS:-4}"
LATENCY_COUNT="${LATENCY_COUNT:-20}"

UA="networkQuality/194.80.3 CFNetwork/3860.400.51 Darwin/25.3.0"
CDN_HOST=""
SELECTED_ENDPOINT_IP=""
SELECTED_ENDPOINT_DESC=""

# ── colour ───────────────────────────────────────────────────
is_tty=0; [ -t 2 ] && is_tty=1
c0="" B="" g="" y="" d="" cy="" r=""
if [ "$is_tty" -eq 1 ]; then
  c0="$(printf '\033[0m')";  B="$(printf '\033[1m')"
  g="$(printf '\033[32m')";  y="$(printf '\033[33m')"
  d="$(printf '\033[2m')";   cy="$(printf '\033[36m')"
  r="$(printf '\033[31m')"
fi

detect_lang() {
  if [ -n "$SPEEDTEST_LANG" ]; then
    _lang="$(printf "%s" "$SPEEDTEST_LANG" | tr '[:upper:]' '[:lower:]')"
    case "$_lang" in
      zh*) printf "zh" ;;
      *) printf "en" ;;
    esac
    return
  fi

  for _lang in "$LC_ALL" "$LC_MESSAGES" "$LANGUAGE" "$LANG"; do
    _lang="$(printf "%s" "$_lang" | tr '[:upper:]' '[:lower:]')"
    case "$_lang" in
      zh*) printf "zh"; return ;;
    esac
  done
  printf "en"
}

LANG_MODE="$(detect_lang)"
is_zh() { [ "$LANG_MODE" = "zh" ]; }

msg() {
  _m="$1"
  if ! is_zh; then
    printf "%s" "$_m"
    return
  fi
  case "$_m" in
    "Environment Check") printf "环境检查" ;;
    "Missing required command: "*) printf "缺少必需命令: %s" "${_m#Missing required command: }" ;;
    "Install hint: "*) printf "安装提示: %s" "${_m#Install hint: }" ;;
    "Environment check failed. Install required tools and rerun.") printf "环境检查失败。请先安装依赖后重试。" ;;
    "curl does not include HTTP/2 support.") printf "curl 未包含 HTTP/2 支持。" ;;
    "A curl build with HTTP/2 support is required.") printf "需要支持 HTTP/2 的 curl 版本。" ;;
    "No DNS helper found (getent/dig/host/nslookup/ping).") printf "未发现 DNS 辅助命令（getent/dig/host/nslookup/ping）。" ;;
    "Server IP detection may be unavailable.") printf "服务端 IP 识别可能不可用。" ;;
    "Optional tool 'pv' not found. Live progress meter is unavailable.") printf "未找到可选工具 pv，无法显示实时进度。" ;;
    "Environment check passed.") printf "环境检查通过。" ;;
    "Endpoint Selection") printf "节点选择" ;;
    "Could not parse host from DL_URL. Skip endpoint selection.") printf "无法从 DL_URL 解析主机，跳过节点选择。" ;;
    "Host: "*) printf "主机: %s" "${_m#Host: }" ;;
    "DoH (CF): "*) printf "DoH (CF): %s" "${_m#DoH (CF): }" ;;
    "DoH (Ali): "*) printf "DoH (Ali): %s" "${_m#DoH (Ali): }" ;;
    "Dual DoH (CF + Ali) both timed out. Fallback to system DNS.") printf "双 DoH（CF + Ali）均超时，回退到系统 DNS。" ;;
    "Dual DoH returned no IPv4 endpoint, continue with default DNS.") printf "双 DoH 未返回 IPv4 节点，继续使用默认 DNS。" ;;
    "Selected endpoint: "*) printf "已选择节点: %s" "${_m#Selected endpoint: }" ;;
    "Could not resolve endpoint IP, continue with default DNS.") printf "无法解析节点 IP，继续使用默认 DNS。" ;;
    "Available endpoints:") printf "可用节点:" ;;
    "No endpoint candidates, continue with default DNS.") printf "没有可用节点，继续使用默认 DNS。" ;;
    "Invalid selection "*) printf "选择无效，回退到 1。(%s)" "${_m#Invalid selection }" ;;
    "Selection out of range, fallback to 1.") printf "选择超出范围，回退到 1。" ;;
    "Non-interactive shell detected, default endpoint 1.") printf "检测到非交互式终端，默认使用节点 1。" ;;
    "Could not parse selected endpoint, continue with default DNS.") printf "无法解析所选节点，继续使用默认 DNS。" ;;
    "Connection Information") printf "连接信息" ;;
    "Client") printf "客户端" ;;
    "Server") printf "服务端" ;;
    "  Location") printf "  位置" ;;
    "  Endpoint") printf "  节点" ;;
    "Idle Latency") printf "空载延迟" ;;
    "Endpoint: "*) printf "端点: %s" "${_m#Endpoint: }" ;;
    "Samples: "*) printf "采样: %s" "${_m#Samples: }" ;;
    "Download (single thread)") printf "下载（单线程）" ;;
    "Download (multi-thread)") printf "下载（多线程）" ;;
    "Upload (single thread)") printf "上传（单线程）" ;;
    "Upload (multi-thread)") printf "上传（多线程）" ;;
    "Threads: "*) printf "线程: %s" "${_m#Threads: }" ;;
    "Limit: "*) printf "上限: %s" "${_m#Limit: }" ;;
    "Loaded latency: "*) printf "负载延迟: %s" "${_m#Loaded latency: }" ;;
    "Data Used") printf "消耗流量" ;;
    "All tests complete.") printf "所有测试完成。" ;;
    "Config:") printf "配置:" ;;
    "Summary") printf "测速汇总" ;;
    *) printf "%s" "$_m" ;;
  esac
}

line(){   printf "%s\n" "${d}────────────────────────────────────────────────────────────${c0}" >&2; }
hdr(){    printf "\n%s%s  ▸ %s%s\n" "$cy" "$B" "$(msg "$*")" "$c0" >&2; }
info(){   printf "  %s%s[+]%s %s\n" "$g" "$B" "$c0" "$(msg "$*")" >&2; }
warn(){   printf "  %s%s[!]%s %s\n" "$y" "$B" "$c0" "$(msg "$*")" >&2; }
result(){ printf "  %s%s    ➜  %s%s\n" "$g" "$B" "$(msg "$*")" "$c0" >&2; }
kv(){     printf "  %s%s%-18s%s %s\n" "$d" "$B" "$(msg "$1"):" "$c0" "$2" >&2; }
fatal(){  printf "  %s%s[✗]%s %s\n" "$r" "$B" "$c0" "$(msg "$*")" >&2; exit 1; }

pkg_install_hint() {
  _pkg="$1"
  if command -v brew >/dev/null 2>&1; then
    printf "brew install %s" "$_pkg"
  elif command -v apt-get >/dev/null 2>&1; then
    printf "sudo apt-get install -y %s" "$_pkg"
  elif command -v dnf >/dev/null 2>&1; then
    printf "sudo dnf install -y %s" "$_pkg"
  elif command -v yum >/dev/null 2>&1; then
    printf "sudo yum install -y %s" "$_pkg"
  elif command -v pacman >/dev/null 2>&1; then
    printf "sudo pacman -S --needed %s" "$_pkg"
  elif command -v apk >/dev/null 2>&1; then
    printf "sudo apk add %s" "$_pkg"
  elif command -v zypper >/dev/null 2>&1; then
    printf "sudo zypper install %s" "$_pkg"
  else
    printf "Install package '%s' via your system package manager" "$_pkg"
  fi
}

require_cmd() {
  _cmd="$1"
  _pkg="$2"
  [ -z "$_pkg" ] && _pkg="$_cmd"
  if command -v "$_cmd" >/dev/null 2>&1; then
    return 0
  fi
  warn "Missing required command: $_cmd"
  warn "Install hint: $(pkg_install_hint "$_pkg")"
  return 1
}

check_environment() {
  hdr "Environment Check"
  _missing=0

  require_cmd curl curl || _missing=1
  require_cmd awk gawk || _missing=1
  require_cmd grep grep || _missing=1
  require_cmd sed sed || _missing=1
  require_cmd sort coreutils || _missing=1
  require_cmd mktemp coreutils || _missing=1
  require_cmd dd coreutils || _missing=1
  require_cmd wc coreutils || _missing=1
  require_cmd tr coreutils || _missing=1
  require_cmd head coreutils || _missing=1

  if [ "$_missing" -ne 0 ]; then
    fatal "Environment check failed. Install required tools and rerun."
  fi

  if ! curl -V 2>/dev/null | grep -qiE 'HTTP2|HTTP/2'; then
    warn "curl does not include HTTP/2 support."
    warn "Install hint: $(pkg_install_hint curl)"
    fatal "A curl build with HTTP/2 support is required."
  fi

  if ! command -v getent >/dev/null 2>&1 \
    && ! command -v dig >/dev/null 2>&1 \
    && ! command -v host >/dev/null 2>&1 \
    && ! command -v nslookup >/dev/null 2>&1 \
    && ! command -v ping >/dev/null 2>&1; then
    warn "No DNS helper found (getent/dig/host/nslookup/ping)."
    warn "Server IP detection may be unavailable."
  fi

  if ! command -v pv >/dev/null 2>&1; then
    warn "Optional tool 'pv' not found. Live progress meter is unavailable."
    warn "Install hint: $(pkg_install_hint pv)"
  fi

  info "Environment check passed."
}

# ── helpers ──────────────────────────────────────────────────
to_bytes() {
  echo "$1" | awk '
  function p1024(n){v=1;for(i=0;i<n;i++)v*=1024;return v}
  function p1000(n){v=1;for(i=0;i<n;i++)v*=1000;return v}
  { s=$0; gsub(/[ \t]/,"",s)
    num=s; sub(/[^0-9.].*$/,"",num)
    u=s;   sub(/^[0-9.]+/,"",u)
    if(u==""){printf "%.0f",num;exit}
    if(u=="K"||u=="KB")printf "%.0f",num*p1000(1);
    else if(u=="M"||u=="MB")printf "%.0f",num*p1000(2);
    else if(u=="G"||u=="GB")printf "%.0f",num*p1000(3);
    else if(u=="T"||u=="TB")printf "%.0f",num*p1000(4);
    else if(u~/^[Kk]i[Bb]$/)printf "%.0f",num*p1024(1);
    else if(u~/^[Mm]i[Bb]$/)printf "%.0f",num*p1024(2);
    else if(u~/^[Gg]i[Bb]$/)printf "%.0f",num*p1024(3);
    else if(u~/^[Tt]i[Bb]$/)printf "%.0f",num*p1024(4);
    else printf "%.0f",num; }'
}

human_bytes() {
  awk -v b="$1" 'BEGIN{
    if(b>=1073741824)printf "%.2f GiB",b/1073741824;
    else if(b>=1048576)printf "%.1f MiB",b/1048576;
    else if(b>=1024)printf "%.0f KiB",b/1024;
    else printf "%d B",b; }'
}

now_sec() {
  _t="$(date +%s.%N 2>/dev/null)"
  case "$_t" in *N) date +%s;; *) echo "$_t";; esac
}

resolve_ip() {
  _host="$1"; _ip=""
  _ip="$(getent ahostsv4 "$_host" 2>/dev/null | awk '{print $1; exit}')"
  [ -z "$_ip" ] && _ip="$(dig +short A "$_host" 2>/dev/null | grep -E '^[0-9]+\.' | head -1)"
  [ -z "$_ip" ] && _ip="$(host "$_host" 2>/dev/null | awk '/has address/{print $4; exit}')"
  [ -z "$_ip" ] && _ip="$(nslookup "$_host" 2>/dev/null | awk '/^Address: /{print $2; exit}')"
  [ -z "$_ip" ] && _ip="$(ping -c1 -W1 "$_host" 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+')"
  echo "$_ip"
}

json_val() {
  awk -v k="\"$1\"" '{
    i=index($0,k)
    if(i>0){
      s=substr($0,i+length(k))
      sub(/^[ \t]*:[ \t]*"?/,"",s)
      sub(/".*$/,"",s)
      gsub(/,$/,"",s)
      print s; exit
    }
  }'
}

apple_curl() {
  if [ -n "$SELECTED_ENDPOINT_IP" ] && [ -n "$CDN_HOST" ]; then
    curl --resolve "${CDN_HOST}:443:${SELECTED_ENDPOINT_IP}" "$@"
  else
    curl "$@"
  fi
}

# ────────────────────────────────────────────────────────────
#  ENDPOINT SELECTION  (CF DoH + AliDNS DoH concurrent)
# ────────────────────────────────────────────────────────────
choose_endpoint() {
  hdr "Endpoint Selection"

  CDN_HOST="$(echo "$DL_URL" | awk -F/ '{print $3}')"
  if [ -z "$CDN_HOST" ]; then
    warn "Could not parse host from DL_URL. Skip endpoint selection."
    return
  fi

  info "Host: $CDN_HOST"
  info "DoH (CF): https://cloudflare-dns.com/dns-query?name=${CDN_HOST}&type=A"
  info "DoH (Ali): https://dns.alidns.com/resolve?name=${CDN_HOST}&type=A"

  # Temp files for concurrent results
  _cf_file="$(mktemp)"
  _ali_file="$(mktemp)"
  _cf_rc_file="$(mktemp)"
  _ali_rc_file="$(mktemp)"

  # Concurrent CF DoH query (background)
  (
    curl -sS --max-time 1 \
      -H 'accept: application/dns-json' \
      "https://cloudflare-dns.com/dns-query?name=${CDN_HOST}&type=A" \
      > "$_cf_file" 2>/dev/null
    echo "$?" > "$_cf_rc_file"
  ) &
  _cf_pid=$!

  # Concurrent Ali DoH query (background)
  (
    curl -sS --max-time 1 \
      "https://dns.alidns.com/resolve?name=${CDN_HOST}&type=A&short=1" \
      > "$_ali_file" 2>/dev/null
    echo "$?" > "$_ali_rc_file"
  ) &
  _ali_pid=$!

  # Wait for both
  wait "$_cf_pid" 2>/dev/null || true
  wait "$_ali_pid" 2>/dev/null || true

  _cf_rc="$(cat "$_cf_rc_file" 2>/dev/null)"; [ -z "$_cf_rc" ] && _cf_rc=1
  _ali_rc="$(cat "$_ali_rc_file" 2>/dev/null)"; [ -z "$_ali_rc" ] && _ali_rc=1

  # Determine timeout status (curl exit code 28 = timeout)
  _cf_timeout=0; [ "$_cf_rc" -eq 28 ] && _cf_timeout=1
  _ali_timeout=0; [ "$_ali_rc" -eq 28 ] && _ali_timeout=1

  # Extract IPs from CF result
  _cf_ips=""
  if [ "$_cf_timeout" -eq 0 ] && [ -s "$_cf_file" ]; then
    _cf_ips="$(grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' "$_cf_file" | awk '!seen[$0]++')"
  fi

  # Extract IPs from Ali result
  _ali_ips=""
  if [ "$_ali_timeout" -eq 0 ] && [ -s "$_ali_file" ]; then
    _ali_ips="$(grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' "$_ali_file" | awk '!seen[$0]++')"
  fi

  rm -f "$_cf_file" "$_ali_file" "$_cf_rc_file" "$_ali_rc_file"

  # Merge: CF first, Ali second, deduplicated
  _ips=""
  if [ -n "$_cf_ips" ] && [ -n "$_ali_ips" ]; then
    _ips="$(printf "%s\n%s\n" "$_cf_ips" "$_ali_ips" | awk '!seen[$0]++')"
  elif [ -n "$_cf_ips" ]; then
    _ips="$_cf_ips"
  elif [ -n "$_ali_ips" ]; then
    _ips="$_ali_ips"
  fi

  if [ -z "$_ips" ]; then
    # Fallback only when BOTH timed out
    if [ "$_cf_timeout" -eq 1 ] && [ "$_ali_timeout" -eq 1 ]; then
      warn "Dual DoH (CF + Ali) both timed out. Fallback to system DNS."
      _fallback_ip="$(resolve_ip "$CDN_HOST")"
      if [ -n "$_fallback_ip" ]; then
        SELECTED_ENDPOINT_IP="$_fallback_ip"
        SELECTED_ENDPOINT_DESC="system DNS fallback"
        info "Selected endpoint: ${SELECTED_ENDPOINT_IP} (${SELECTED_ENDPOINT_DESC})"
      else
        warn "Could not resolve endpoint IP, continue with default DNS."
      fi
    else
      warn "Dual DoH returned no IPv4 endpoint, continue with default DNS."
      warn "Could not resolve endpoint IP, continue with default DNS."
    fi
    return
  fi

  _ips_file="$(mktemp)"
  _map_file="$(mktemp)"
  printf "%s\n" "$_ips" > "$_ips_file"

  info "Available endpoints:"
  _idx=1
  while IFS= read -r _ip; do
    [ -z "$_ip" ] && continue

    _meta="$(curl -sS --max-time 4 \
      "http://ip-api.com/json/${_ip}?fields=status,message,city,regionName,country,as,org" \
      2>/dev/null || true)"
    _status="$(echo "$_meta" | json_val "status")"
    _city="$(echo "$_meta" | json_val "city")"
    _region="$(echo "$_meta" | json_val "regionName")"
    _country="$(echo "$_meta" | json_val "country")"
    _org="$(echo "$_meta" | json_val "org")"
    _as="$(echo "$_meta" | json_val "as")"

    _loc=""
    if [ "$_status" = "success" ]; then
      _loc="${_city}"
      [ -n "$_region" ] && [ "$_region" != "$_city" ] && _loc="${_loc}, ${_region}"
      [ -n "$_country" ] && _loc="${_loc}, ${_country}"
      [ -z "$_loc" ] && _loc="unknown location"
    else
      _loc="lookup failed"
    fi

    [ -z "$_as" ] && _as="$_org"
    _desc="$_loc"
    [ -n "$_as" ] && _desc="${_desc} (${_as})"

    printf "  %s) %s  %s\n" "$_idx" "$_ip" "$_desc" >&2
    printf "%s|%s|%s\n" "$_idx" "$_ip" "$_desc" >> "$_map_file"
    _idx=$((_idx + 1))
  done < "$_ips_file"

  _count=$((_idx - 1))
  if [ "$_count" -le 0 ]; then
    rm -f "$_ips_file" "$_map_file"
    warn "No endpoint candidates, continue with default DNS."
    return
  fi

  _choice=1
  if [ "$_count" -gt 1 ]; then
    if [ "$is_tty" -eq 1 ] && tty -s 2>/dev/null && [ -r /dev/tty ] && [ -w /dev/tty ]; then
      if is_zh; then
        printf "  %s%s[?]%s 选择节点 [1-%s，回车=1]: " "$cy" "$B" "$c0" "$_count" > /dev/tty
      else
        printf "  %s%s[?]%s Select endpoint [1-%s, Enter=1]: " "$cy" "$B" "$c0" "$_count" > /dev/tty
      fi
      IFS= read -r _pick < /dev/tty 2>/dev/null || _pick=""
      case "$_pick" in
        "") _choice=1 ;;
        *[!0-9]*)
          warn "Invalid selection '$_pick', fallback to 1."
          _choice=1
          ;;
        *)
          if [ "$_pick" -ge 1 ] && [ "$_pick" -le "$_count" ]; then
            _choice="$_pick"
          else
            warn "Selection out of range, fallback to 1."
            _choice=1
          fi
          ;;
      esac
    else
      warn "Non-interactive shell detected, default endpoint 1."
    fi
  fi

  _selected="$(awk -F'|' -v c="$_choice" '$1==c{print; exit}' "$_map_file")"
  SELECTED_ENDPOINT_IP="$(echo "$_selected" | awk -F'|' '{print $2}')"
  SELECTED_ENDPOINT_DESC="$(echo "$_selected" | awk -F'|' '{print $3}')"

  if [ -n "$SELECTED_ENDPOINT_IP" ]; then
    info "Selected endpoint: ${SELECTED_ENDPOINT_IP} (${SELECTED_ENDPOINT_DESC})"
  else
    warn "Could not parse selected endpoint, continue with default DNS."
  fi

  rm -f "$_ips_file" "$_map_file"
}

# ────────────────────────────────────────────────────────────
#  CONNECTION INFO
# ────────────────────────────────────────────────────────────
gather_info() {
  hdr "Connection Information"

  client_json="$(curl -sS --max-time 5 \
    "http://ip-api.com/json/?fields=query,as,isp,city,regionName,country" 2>/dev/null || true)"
  if [ -n "$client_json" ]; then
    client_ip="$(echo "$client_json"      | json_val "query")"
    client_as="$(echo "$client_json"      | json_val "as")"
    client_isp="$(echo "$client_json"     | json_val "isp")"
    client_city="$(echo "$client_json"    | json_val "city")"
    client_region="$(echo "$client_json"  | json_val "regionName")"
    client_country="$(echo "$client_json" | json_val "country")"
    client_loc="${client_city}"
    [ -n "$client_region" ] && [ "$client_region" != "$client_city" ] && \
      client_loc="${client_loc}, ${client_region}"
    [ -n "$client_country" ] && client_loc="${client_loc}, ${client_country}"
  else
    client_ip="?"; client_as="?"; client_isp="?"; client_loc="?"
  fi

  server_host="$CDN_HOST"
  [ -z "$server_host" ] && server_host="$(echo "$DL_URL" | awk -F/ '{print $3}')"
  if [ -n "$SELECTED_ENDPOINT_IP" ]; then
    server_ip="$SELECTED_ENDPOINT_IP"
  else
    server_ip="$(resolve_ip "$server_host")"
  fi

  if [ -n "$server_ip" ]; then
    srv_json="$(curl -sS --max-time 5 \
      "http://ip-api.com/json/${server_ip}?fields=query,as,org,city,country" 2>/dev/null || true)"
    server_as="$(echo "$srv_json"      | json_val "as")"
    server_org="$(echo "$srv_json"     | json_val "org")"
    server_city="$(echo "$srv_json"    | json_val "city")"
    server_country="$(echo "$srv_json" | json_val "country")"
    server_loc=""
    [ -n "$server_city" ]    && server_loc="${server_city}"
    [ -n "$server_country" ] && server_loc="${server_loc}, ${server_country}"
  else
    server_ip="?"; server_as="?"; server_org="?"; server_loc="?"
  fi

  kv "Client" "${client_ip}  (${client_isp})"
  kv "  ASN" "${client_as}"
  kv "  Location" "${client_loc}"
  kv "Server" "${server_host}  →  ${server_ip}"
  [ -n "$SELECTED_ENDPOINT_DESC" ] && kv "  Endpoint" "$SELECTED_ENDPOINT_DESC"
  kv "  ASN" "${server_as}"
  kv "  Location" "${server_loc}"
}

# ────────────────────────────────────────────────────────────
#  IDLE LATENCY
# ────────────────────────────────────────────────────────────
test_idle_latency() {
  hdr "Idle Latency"
  info "Endpoint: $LATENCY_URL"
  info "Samples: $LATENCY_COUNT"

  tmpf="$(mktemp)"
  i=0
  while [ "$i" -lt "$LATENCY_COUNT" ]; do
    apple_curl --http2 -sS -o /dev/null --max-time 3 \
      -H "user-agent: ${UA}" \
      -H "accept: */*" \
      -H "accept-language: zh-CN,zh-Hans;q=0.9" \
      -H "accept-encoding: identity" \
      -H "priority: u=3, i" \
      -w "%{time_total}\n" "$LATENCY_URL" >> "$tmpf" 2>/dev/null || true
    i=$((i + 1))
  done

  stats="$(sort -n "$tmpf" | awk '
    { a[NR]=$1*1000; sum+=a[NR] }
    END {
      n=NR; if(n==0){print "0 0 0 0 0";exit}
      min=a[1]; max=a[n]; avg=sum/n
      if(n%2==1) med=a[int(n/2)+1]; else med=(a[n/2]+a[n/2+1])/2
      jit=0; for(i=2;i<=n;i++){d=a[i]-a[i-1];if(d<0)d=-d;jit+=d}
      if(n>1)jit/=(n-1)
      printf "%.2f %.2f %.2f %.2f %.2f",min,avg,med,max,jit
    }')"

  lat_min="$(echo "$stats" | awk '{print $1}')"
  lat_avg="$(echo "$stats" | awk '{print $2}')"
  lat_med="$(echo "$stats" | awk '{print $3}')"
  lat_max="$(echo "$stats" | awk '{print $4}')"
  lat_jit="$(echo "$stats" | awk '{print $5}')"
  rm -f "$tmpf"

  IDLE_LATENCY="$lat_med"; IDLE_JITTER="$lat_jit"
  if is_zh; then
    result "${lat_med} 毫秒 中位数  (最小 ${lat_min} / 平均 ${lat_avg} / 最大 ${lat_max})  抖动 ${lat_jit} 毫秒"
  else
    result "${lat_med} ms median  (min ${lat_min} / avg ${lat_avg} / max ${lat_max})  jitter ${lat_jit} ms"
  fi
}

# ────────────────────────────────────────────────────────────
#  LOADED LATENCY  (background probe)
# ────────────────────────────────────────────────────────────
start_loaded_latency() {
  LOADED_LAT_FILE="$(mktemp)"; LOADED_LAT_PID=""
  (
    trap 'exit 0' TERM INT HUP
    while true; do
      apple_curl --http2 -sS -o /dev/null --max-time 2 \
        -H "user-agent: ${UA}" \
        -H "accept: */*" \
        -H "accept-language: zh-CN,zh-Hans;q=0.9" \
        -H "accept-encoding: identity" \
        -H "priority: u=3, i" \
        -w "%{time_total}\n" "$LATENCY_URL" >> "$LOADED_LAT_FILE" 2>/dev/null || true
    done
  ) &
  LOADED_LAT_PID=$!
}

stop_loaded_latency() {
  [ -n "$LOADED_LAT_PID" ] && kill "$LOADED_LAT_PID" 2>/dev/null
  wait "$LOADED_LAT_PID" 2>/dev/null || true; LOADED_LAT_PID=""

  if [ -s "$LOADED_LAT_FILE" ]; then
    sort -n "$LOADED_LAT_FILE" | awk '
      { a[NR]=$1*1000; sum+=a[NR] }
      END {
        n=NR; if(n==0){print "? ?";exit}
        if(n%2==1) med=a[int(n/2)+1]; else med=(a[n/2]+a[n/2+1])/2
        jit=0; for(i=2;i<=n;i++){d=a[i]-a[i-1];if(d<0)d=-d;jit+=d}
        if(n>1)jit/=(n-1)
        printf "%.2f %.2f",med,jit
      }'
  else
    printf "? ?"
  fi
  rm -f "$LOADED_LAT_FILE"
}

# ────────────────────────────────────────────────────────────
#  DOWNLOAD  — one connection
#
#  核心策略：
#    curl --max-time $TIMEOUT  → 到时间自动停
#    curl -w '%{size_download} %{time_total}'  → 拿到真实字节数
#    不用 head 截断（会导致 curl 报 error 23，-w 拿不到值）
#    不用 dd 计数（stderr 格式不可控）
#    不用 sh -c 嵌套（kill 不到子进程）
#
#    但我们仍需限制最大字节数（2G）：
#      → 用 --limit-rate 不行（会限速）
#      → 用 -r Range 不行（暴露非官方）
#      → 解决方案：写到临时文件，curl 后台跑，
#        主进程每秒检查文件大小，达到 MAX 就 kill
# ────────────────────────────────────────────────────────────
do_download_one() {
  _maxb="$1"
  _tmpfile="$(mktemp)"
  _timefile="$(mktemp)"

  # 后台启动 curl，写入临时文件
  apple_curl --http2 -sS -L \
    -H "user-agent: ${UA}" \
    -H "accept: */*" \
    -H "accept-language: zh-CN,zh-Hans;q=0.9" \
    -H "accept-encoding: identity" \
    -H "priority: u=3, i" \
    --max-time "$TIMEOUT" \
    -o "$_tmpfile" \
    -w "%{size_download} %{time_total}" \
    "$DL_URL" > "$_timefile" 2>/dev/null &
  _curl_pid=$!

  # 监控文件大小，到 MAX 就 kill
  while kill -0 "$_curl_pid" 2>/dev/null; do
    _cur="$(wc -c < "$_tmpfile" 2>/dev/null | tr -d ' ')"
    [ -z "$_cur" ] && _cur=0
    if [ "$_cur" -ge "$_maxb" ] 2>/dev/null; then
      kill "$_curl_pid" 2>/dev/null
      break
    fi
    sleep 0.2 2>/dev/null || sleep 1
  done
  wait "$_curl_pid" 2>/dev/null || true

  # 获取实际字节数：优先用 -w 输出，回退用 wc -c
  _wout="$(cat "$_timefile" 2>/dev/null)"
  _dl_bytes="$(echo "$_wout" | awk '{print int($1)}')"
  _dl_time="$(echo "$_wout" | awk '{print $2}')"

  # 如果 -w 没拿到（curl 被 kill 时可能为空），用 wc -c
  if [ -z "$_dl_bytes" ] || [ "$_dl_bytes" -le 0 ] 2>/dev/null; then
    _dl_bytes="$(wc -c < "$_tmpfile" 2>/dev/null | tr -d ' ')"
  fi
  [ -z "$_dl_bytes" ] && _dl_bytes=0

  rm -f "$_tmpfile" "$_timefile"
  echo "$_dl_bytes"
}

# ────────────────────────────────────────────────────────────
#  UPLOAD  — one connection
#
#  策略：dd 生成数据 | curl -T - 流式上传
#  用 --max-time 控制超时，curl -w 拿实际上传字节
#  避免 --data-binary 在大输入场景下可能触发 OOM
# ────────────────────────────────────────────────────────────
do_upload_one() {
  _maxb="$1"
  _blocks=$((_maxb / 1048576))
  [ "$_blocks" -le 0 ] && _blocks=1
  _resfile="$(mktemp)"

  # 用 -T - 进行流式上传，避免 --data-binary 在大输入下触发 OOM
  # 管道放在子 shell 里并静默，避免上层 shell 打出 Broken pipe/Killed 噪声
  (
    dd if=/dev/zero bs=1M count="$_blocks" 2>/dev/null \
    | apple_curl --http2 -sS \
        -H "user-agent: ${UA}" \
        -H "accept: */*" \
        -H "accept-language: zh-CN,zh-Hans;q=0.9" \
        -H "accept-encoding: identity" \
        -H "priority: u=3, i" \
        -H "upload-draft-interop-version: 6" \
        -H "upload-complete: ?1" \
        --max-time "$TIMEOUT" \
        -T - \
        -o /dev/null \
        -w "%{size_upload} %{time_total}" \
        "$UL_URL" > "$_resfile" 2>/dev/null
  ) >/dev/null 2>&1 || true

  _wout="$(cat "$_resfile" 2>/dev/null)"
  _ul_bytes="$(echo "$_wout" | awk '{print int($1)}')"
  rm -f "$_resfile"

  [ -z "$_ul_bytes" ] && _ul_bytes=0
  echo "$_ul_bytes"
}

# ────────────────────────────────────────────────────────────
#  TRANSFER TEST  (generic wrapper)
# ────────────────────────────────────────────────────────────
run_transfer_test() {
  _dir="$1"; _threads="$2"; _label="$3"
  hdr "$_label"
  _maxb="$(to_bytes "$MAX")"

  info "Threads: $_threads"
  if is_zh; then
    info "Limit: $MAX / 每线程 ${TIMEOUT}s"
  else
    info "Limit: $MAX / ${TIMEOUT}s per thread"
  fi

  start_loaded_latency

  _tmpdir="$(mktemp -d)"
  _wall_start="$(now_sec)"

  _worker_pids=""
  _j=0
  while [ "$_j" -lt "$_threads" ]; do
    (
      if [ "$_dir" = "download" ]; then
        do_download_one "$_maxb"
      else
        do_upload_one "$_maxb"
      fi
    ) > "${_tmpdir}/t${_j}" &
    _wpid=$!
    _worker_pids="${_worker_pids} ${_wpid}"
    _j=$((_j + 1))
  done
  for _wpid in $_worker_pids; do
    wait "$_wpid" 2>/dev/null || true
  done

  _wall_end="$(now_sec)"
  _wall="$(awk -v s="$_wall_start" -v e="$_wall_end" 'BEGIN{d=e-s;if(d<=0)d=1;printf "%.1f",d}')"

  _ll="$(stop_loaded_latency)"
  _ll_med="$(echo "$_ll" | awk '{print $1}')"
  _ll_jit="$(echo "$_ll" | awk '{print $2}')"

  _total_bytes=0
  _j=0
  while [ "$_j" -lt "$_threads" ]; do
    _b="$(cat "${_tmpdir}/t${_j}" 2>/dev/null | tr -d ' \n')"
    if [ -n "$_b" ] && [ "$_b" -gt 0 ] 2>/dev/null; then
      _total_bytes=$((_total_bytes + _b))
    fi
    _j=$((_j + 1))
  done
  rm -rf "$_tmpdir"

  _mbps="$(awk -v b="$_total_bytes" -v t="$_wall" 'BEGIN{if(t<=0)t=1; printf "%.0f",(b*8)/(t*1000000.0)}')"
  _human="$(human_bytes "$_total_bytes")"
  TOTAL_DATA=$((TOTAL_DATA + _total_bytes))

  if [ "$_threads" -le 1 ]; then
    if is_zh; then
      result "${_mbps} Mbps  (${_human}，耗时 ${_wall}s)"
    else
      result "${_mbps} Mbps  (${_human} in ${_wall}s)"
    fi
  else
    if is_zh; then
      result "${_mbps} Mbps  (${_human}，耗时 ${_wall}s，${_threads} 线程)"
    else
      result "${_mbps} Mbps  (${_human} in ${_wall}s, ${_threads} threads)"
    fi
  fi
  if is_zh; then
    info "Loaded latency: ${_ll_med} 毫秒  (抖动 ${_ll_jit} 毫秒)"
  else
    info "Loaded latency: ${_ll_med} ms  (jitter ${_ll_jit} ms)"
  fi
}

# ============================================================
#  MAIN
# ============================================================
TOTAL_DATA=0

printf "\n" >&2
line
if is_zh; then
  printf "  %s%s⚡ iNetSpeed-CLI%s\n" "$cy" "$B" "$c0" >&2
  printf "  %s%s%s%s  timeout=%ss  max=%s  threads=%s\n" "$d" "$B" "$(msg "Config:")" "$c0" "$TIMEOUT" "$MAX" "$THREADS" >&2
else
  printf "  %s%s⚡ iNetSpeed-CLI%s\n" "$cy" "$B" "$c0" >&2
  printf "  %s%sConfig:%s  timeout=%ss  max=%s  threads=%s\n" "$d" "$B" "$c0" "$TIMEOUT" "$MAX" "$THREADS" >&2
fi
line

check_environment
choose_endpoint
gather_info
test_idle_latency
run_transfer_test download 1          "Download (single thread)"
run_transfer_test download "$THREADS"  "Download (multi-thread)"
run_transfer_test upload   1          "Upload (single thread)"
run_transfer_test upload   "$THREADS"  "Upload (multi-thread)"

# ── Summary ──────────────────────────────────────────────────
total_human="$(human_bytes "$TOTAL_DATA")"

printf "\n" >&2
line
printf "  %s%s📊 %s%s\n" "$cy" "$B" "$(msg "Summary")" "$c0" >&2
line
if is_zh; then
  kv "Idle Latency" "${IDLE_LATENCY} 毫秒  (抖动 ${IDLE_JITTER} 毫秒)"
else
  kv "Idle Latency" "${IDLE_LATENCY} ms  (jitter ${IDLE_JITTER} ms)"
fi
kv "Data Used" "$total_human"
line
info "All tests complete."
line
printf "\n" >&2
