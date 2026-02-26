#!/bin/sh
set -f

URL="${URL:-https://mensura.cdn-apple.com/api/v1/gm/slurp}"
SIZE="${SIZE:-2G}"
TIMEOUT="${TIMEOUT:-10}"

is_tty=0; [ -t 2 ] && is_tty=1
c0=""; b=""; g=""; y=""; d=""; r=""
if [ "$is_tty" -eq 1 ]; then
  c0="$(printf '\033[0m')"; b="$(printf '\033[1m')"
  g="$(printf '\033[32m')"; y="$(printf '\033[33m')"; d="$(printf '\033[2m')"; r="$(printf '\033[31m')"
fi
line(){ printf "%s\n" "${d}------------------------------------------------------------${c0}" >&2; }
info(){ printf "%s%s[+]%s %s\n" "$g" "$b" "$c0" "$*" >&2; }
warn(){ printf "%s%s[!]%s %s\n" "$y" "$b" "$c0" "$*" >&2; }
fatal(){ printf "%s%s[x]%s %s\n" "$r" "$b" "$c0" "$*" >&2; exit 1; }

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
  _cmd="$1"; _pkg="$2"
  [ -z "$_pkg" ] && _pkg="$_cmd"
  if command -v "$_cmd" >/dev/null 2>&1; then
    return 0
  fi
  warn "Missing required command: $_cmd"
  warn "Install hint: $(pkg_install_hint "$_pkg")"
  return 1
}

check_environment() {
  _missing=0
  require_cmd curl curl || _missing=1
  require_cmd awk gawk || _missing=1
  require_cmd dd coreutils || _missing=1
  require_cmd mktemp coreutils || _missing=1
  require_cmd date coreutils || _missing=1
  require_cmd cat coreutils || _missing=1
  if [ "$_missing" -ne 0 ]; then
    fatal "Environment check failed. Install required tools and rerun."
  fi
  if ! command -v pv >/dev/null 2>&1; then
    warn "Optional tool 'pv' not found."
    warn "Install hint: $(pkg_install_hint pv)"
  fi
}

check_environment

HTTP="--http2"
curl -V 2>/dev/null | grep -qi HTTP2 || HTTP=""

HAS_PV=0; command -v pv >/dev/null 2>&1 && HAS_PV=1

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

line
info "Apple-CDN upload test"
info "URL: $URL"
info "Limit: $SIZE / ${TIMEOUT}s (whichever first)"
info "Meter: $( [ "$HAS_PV" -eq 1 ] && echo pv || echo curl )"
line

if [ "$HAS_PV" -eq 0 ]; then
  warn "pv not found. No live progress will be shown."
  warn "Install it for real-time display:  apt install pv -y"
  warn "Testing in progress, please wait..."
  line
fi

size_bytes="$(to_bytes "$SIZE")"
blocks=$((size_bytes / 1048576))
[ "$blocks" -le 0 ] && blocks=1

if [ "$HAS_PV" -eq 1 ]; then
  countfile="$(mktemp)"

  start="$(date +%s.%N 2>/dev/null || date +%s)"

  sh -c '
    dd if=/dev/zero bs=1M count='"$blocks"' 2>/dev/null \
      | pv -s "'"$size_bytes"'" \
      | pv -n -b -i 0.2 2>"'"$countfile"'" \
      | curl '"$HTTP"' -sS -o /dev/null -T - "'"$URL"'" 2>/dev/null
  ' &
  pipe_pid=$!

  ( sleep "$TIMEOUT" 2>/dev/null; kill $pipe_pid 2>/dev/null ) &
  timer_pid=$!

  wait $pipe_pid 2>/dev/null || true
  kill $timer_pid 2>/dev/null; wait $timer_pid 2>/dev/null || true

  end="$(date +%s.%N 2>/dev/null || date +%s)"

  actual_bytes="$(awk 'NF{v=$1} END{gsub(/[^0-9]/,"",v); print v+0}' "$countfile" 2>/dev/null)"
  rm -f "$countfile"
  [ -z "$actual_bytes" ] && actual_bytes=0

  secs="$(awk -v s="$start" -v e="$end" 'BEGIN{d=e-s; if(d<=0)d=1; printf "%.1f",d}')"
  mbps="$(awk -v b="$actual_bytes" -v t="$secs" 'BEGIN{if(t<=0)t=1; printf "%.0f",(b*8)/(t*1000000.0)}')"
  human="$(human_bytes "$actual_bytes")"

  printf "\n%s%sResult:%s %s in %ss  →  %s Mbps\n" "$g" "$b" "$c0" "$human" "$secs" "$mbps" >&2

else
  tmpout="$(mktemp)"

  sh -c '
    dd if=/dev/zero bs=1M count='"$blocks"' 2>/dev/null \
      | curl '"$HTTP"' -sS -o /dev/null -T - "'"$URL"'" 2>/dev/null \
        -w "time=%{time_total} size=%{size_upload} speed=%{speed_upload}\n" \
      >"'"$tmpout"'"
  ' &
  pipe_pid=$!
  ( sleep "$TIMEOUT" 2>/dev/null; kill $pipe_pid 2>/dev/null ) &
  timer_pid=$!
  wait $pipe_pid 2>/dev/null || true
  kill $timer_pid 2>/dev/null; wait $timer_pid 2>/dev/null || true

  out="$(cat "$tmpout")"; rm -f "$tmpout"

  t="$(echo "$out" | awk -F'[ =]' '{for(i=1;i<=NF;i++)if($i=="time")print $(i+1)}')"
  sz="$(echo "$out" | awk -F'[ =]' '{for(i=1;i<=NF;i++)if($i=="size")print $(i+1)}')"

  if [ -n "$sz" ] && [ -n "$t" ]; then
    mbps="$(awk -v b="$sz" -v t="$t" 'BEGIN{if(t<=0)t=1; printf "%.0f",(b*8)/(t*1000000.0)}')"
    human="$(human_bytes "${sz%.*}")"
    printf "\n%s%sResult:%s %s in %ss  →  %s Mbps\n" "$g" "$b" "$c0" "$human" "$t" "$mbps" >&2
  else
    warn "Could not retrieve transfer stats."
  fi
fi

line
info "Done"
