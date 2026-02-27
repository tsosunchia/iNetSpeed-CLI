#!/usr/bin/env bash
set -euo pipefail

# ============================================================
#  Unit tests for apple-cdn-speedtest.sh helper functions
#  Usage:  bash scripts/apple-cdn-speedtest_test.sh
# ============================================================

PASS=0
FAIL=0

assert_eq() {
  _label="$1"; _got="$2"; _want="$3"
  if [ "$_got" = "$_want" ]; then
    printf "  PASS: %s\n" "$_label"
    PASS=$((PASS + 1))
  else
    printf "  FAIL: %s\n    got:  %s\n    want: %s\n" "$_label" "$_got" "$_want"
    FAIL=$((FAIL + 1))
  fi
}

assert_contains() {
  _label="$1"; _haystack="$2"; _needle="$3"
  case "$_haystack" in
    *"$_needle"*)
      printf "  PASS: %s\n" "$_label"
      PASS=$((PASS + 1))
      ;;
    *)
      printf "  FAIL: %s (not found: %s)\n" "$_label" "$_needle"
      FAIL=$((FAIL + 1))
      ;;
  esac
}

assert_not_contains() {
  _label="$1"; _haystack="$2"; _needle="$3"
  case "$_haystack" in
    *"$_needle"*)
      printf "  FAIL: %s (found: %s)\n" "$_label" "$_needle"
      FAIL=$((FAIL + 1))
      ;;
    *)
      printf "  PASS: %s\n" "$_label"
      PASS=$((PASS + 1))
      ;;
  esac
}

# Source the main script in test mode (functions only, no execution)
SPEEDTEST_SOURCE_ONLY=1
# Temporarily allow unset variables — the main script doesn't use set -u.
set +u
# shellcheck disable=SC1090
. "$(dirname "$0")/apple-cdn-speedtest.sh"
set -u

echo "=== ip_api_lang_suffix tests ==="

# Test: Chinese mode
LANG_MODE="zh"
_suffix="$(ip_api_lang_suffix)"
assert_eq "zh suffix" "$_suffix" "&lang=zh-CN"

# Test: English mode
LANG_MODE="en"
_suffix="$(ip_api_lang_suffix)"
assert_eq "en suffix" "$_suffix" ""

echo ""
echo "=== ip_api_url tests ==="

# Test: Chinese mode with target
LANG_MODE="zh"
_url="$(ip_api_url "1.2.3.4" "status,city")"
assert_eq "zh url with target" "$_url" "http://ip-api.com/json/1.2.3.4?fields=status,city&lang=zh-CN"

# Test: English mode with target
LANG_MODE="en"
_url="$(ip_api_url "1.2.3.4" "status,city")"
assert_eq "en url with target" "$_url" "http://ip-api.com/json/1.2.3.4?fields=status,city"

# Test: Chinese mode self-lookup
LANG_MODE="zh"
_url="$(ip_api_url "" "status,query")"
assert_eq "zh url self" "$_url" "http://ip-api.com/json/?fields=status,query&lang=zh-CN"

# Test: English mode self-lookup
LANG_MODE="en"
_url="$(ip_api_url "" "status,query")"
assert_eq "en url self" "$_url" "http://ip-api.com/json/?fields=status,query"

echo ""
echo "=== msg() DoH entry removal tests ==="

# After removing DoH entries, msg() should pass through DoH strings as-is in zh
LANG_MODE="zh"
_m1="$(msg "DoH (CF): test")"
# Without a dedicated case, the catch-all (*) just returns the string unchanged.
assert_eq "zh DoH CF passthrough" "$_m1" "DoH (CF): test"

_m2="$(msg "DoH (Ali): test")"
assert_eq "zh DoH Ali passthrough" "$_m2" "DoH (Ali): test"

echo ""
echo "=== no-endpoint text tests ==="

LANG_MODE="en"
_m3="$(msg "Dual DoH returned no endpoint, continue with default DNS.")"
assert_eq "no-endpoint en" "$_m3" "Dual DoH returned no endpoint, continue with default DNS."

LANG_MODE="zh"
_m4="$(msg "Dual DoH returned no endpoint, continue with default DNS.")"
assert_eq "no-endpoint zh" "$_m4" "双 DoH 未返回节点，继续使用默认 DNS。"

echo ""
echo "=== choose_endpoint merge order test ==="

# Mock curl to simulate DoH responses
curl() {
  case "$*" in
    *cloudflare-dns.com*type=AAAA*)
      echo '{"Answer":[{"data":"2001:db8::cf"}]}'
      ;;
    *cloudflare-dns.com*type=A*)
      echo '{"Answer":[{"data":"1.1.1.1"}]}'
      ;;
    *dns.alidns.com*type=AAAA*)
      echo '{"Answer":[{"data":"2001:db8::ace"}]}'
      ;;
    *dns.alidns.com*type=A*)
      echo '{"Answer":[{"data":"2.2.2.2"}]}'
      ;;
    *ip-api.com*)
      echo '{"status":"success","city":"Tokyo","regionName":"Tokyo","country":"Japan","as":"AS1234","org":"Test"}'
      ;;
    *) return 0 ;;
  esac
}
export -f curl

DL_URL="https://test.example.com/path"
CDN_HOST=""
SELECTED_ENDPOINT_IP=""
SELECTED_ENDPOINT_DESC=""
is_tty=0

_test_out="$(mktemp)"
set +eu
choose_endpoint 2>"$_test_out"
set -eu
_output="$(cat "$_test_out")"; rm -f "$_test_out"

# Verify merge order: CF-A, CF-AAAA, Ali-A, Ali-AAAA
_line1="$(echo "$_output" | grep -E '^\s+1\)' | head -1)"
_line2="$(echo "$_output" | grep -E '^\s+2\)' | head -1)"
_line3="$(echo "$_output" | grep -E '^\s+3\)' | head -1)"
_line4="$(echo "$_output" | grep -E '^\s+4\)' | head -1)"

assert_contains "order: CF-A first" "$_line1" "1.1.1.1"
assert_contains "order: CF-AAAA second" "$_line2" "2001:db8::cf"
assert_contains "order: Ali-A third" "$_line3" "2.2.2.2"
assert_contains "order: Ali-AAAA fourth" "$_line4" "2001:db8::ace"

# Verify no DoH URL paths in output
assert_not_contains "no dns-query in output" "$_output" "dns-query"
assert_not_contains "no resolve?name= in output" "$_output" "resolve?name="

unset -f curl

LANG_MODE="en"

echo ""
echo "=== fallback semantics test (both providers timeout) ==="

# Mock curl: all DoH queries "timeout" (exit code 28)
curl() {
  case "$*" in
    *cloudflare-dns.com*|*dns.alidns.com*)
      return 28
      ;;
    *ip-api.com*)
      echo '{"status":"success","city":"Fallback","regionName":"FB","country":"US","as":"AS0","org":"FBOrg"}'
      ;;
    *) return 0 ;;
  esac
}
export -f curl

# Mock resolve_ip for system DNS fallback
resolve_ip() { echo "9.9.9.9"; }

CDN_HOST=""
SELECTED_ENDPOINT_IP=""
SELECTED_ENDPOINT_DESC=""
is_tty=0
DL_URL="https://test.example.com/path"

_test_out="$(mktemp)"
set +eu
choose_endpoint 2>"$_test_out"
set -eu
_output="$(cat "$_test_out")"; rm -f "$_test_out"
assert_contains "dual-timeout fallback triggered" "$_output" "Dual DoH"
assert_eq "fallback IP" "$SELECTED_ENDPOINT_IP" "9.9.9.9"

unset -f curl
unset -f resolve_ip

echo ""
echo "=== fallback semantics test (only CF timeout, Ali succeeds) ==="

curl() {
  case "$*" in
    *cloudflare-dns.com*)
      return 28
      ;;
    *dns.alidns.com*type=AAAA*)
      echo '{"Answer":[]}'
      ;;
    *dns.alidns.com*type=A*)
      echo '{"Answer":[{"data":"3.3.3.3"}]}'
      ;;
    *ip-api.com*)
      echo '{"status":"success","city":"Test","regionName":"Test","country":"Test","as":"AS1","org":"T"}'
      ;;
    *) return 0 ;;
  esac
}
export -f curl

CDN_HOST=""
SELECTED_ENDPOINT_IP=""
SELECTED_ENDPOINT_DESC=""
is_tty=0
DL_URL="https://test.example.com/path"

_test_out="$(mktemp)"
set +eu
choose_endpoint 2>"$_test_out"
set -eu
_output="$(cat "$_test_out")"; rm -f "$_test_out"
assert_eq "partial-timeout no-fallback IP" "$SELECTED_ENDPOINT_IP" "3.3.3.3"

unset -f curl

echo ""
echo "=== Summary ==="
echo "PASS: $PASS  FAIL: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
echo "All shell tests passed."
