package endpoint

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var ipv6Re = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}`)

var (
	cfDoHURLTemplate      = "https://cloudflare-dns.com/dns-query?name=%s&type=A"
	cfDoHAAAAURLTemplate  = "https://cloudflare-dns.com/dns-query?name=%s&type=AAAA"
	aliDoHURLTemplate     = "https://dns.alidns.com/resolve?name=%s&type=A&short=1"
	aliDoHAAAAURLTemplate = "https://dns.alidns.com/resolve?name=%s&type=AAAA&short=1"

	// dohTimeout is the per-provider timeout for DoH queries.
	dohTimeout = 1 * time.Second

	dohHTTPClient     = http.DefaultClient
	resolveDoHFn      = resolveDoHDual
	resolveSystemFn   = resolveSystem
	fetchIPDescFn     = fetchIPDesc
	openPromptInputFn = openPromptInput
)

type Endpoint struct {
	IP   string
	Desc string
}

type IPInfo struct {
	Status     string `json:"status"`
	Query      string `json:"query"`
	AS         string `json:"as"`
	ISP        string `json:"isp"`
	Org        string `json:"org"`
	City       string `json:"city"`
	RegionName string `json:"regionName"`
	Country    string `json:"country"`
}

// dohResult holds the outcome of a single DoH provider query.
type dohResult struct {
	ips      []string
	timedOut bool
	err      error
}

func Choose(ctx context.Context, host string, bus *render.Bus, isTTY bool) Endpoint {
	bus.Header(i18n.Text("Endpoint Selection", "节点选择"))
	if host == "" {
		bus.Warn(i18n.Text("Could not parse host from DL_URL. Skip endpoint selection.", "无法从 DL_URL 解析主机，跳过节点选择。"))
		return Endpoint{}
	}
	bus.Info(i18n.Text("Host: ", "主机: ") + host)

	ips, cfTimedOut, aliTimedOut := resolveDoHFn(ctx, host)
	if len(ips) == 0 {
		if cfTimedOut && aliTimedOut {
			bus.Warn(i18n.Text("Dual DoH (CF + Ali) both timed out. Fallback to system DNS.", "双 DoH（CF + Ali）均超时，回退系统 DNS。"))
			fb := resolveSystemFn(host)
			if fb != "" {
				ep := Endpoint{IP: fb, Desc: i18n.Text("system DNS fallback", "系统 DNS 回退")}
				bus.Info(i18n.Text("Selected endpoint: ", "已选择节点: ") + ep.IP + " (" + ep.Desc + ")")
				return ep
			}
			bus.Warn(i18n.Text("Could not resolve endpoint IP, continue with default DNS.", "无法解析节点 IP，继续使用默认 DNS。"))
			return Endpoint{}
		}
		bus.Warn(i18n.Text("Dual DoH returned no endpoint, continue with default DNS.", "双 DoH 未返回节点，继续使用默认 DNS。"))
		bus.Warn(i18n.Text("Could not resolve endpoint IP, continue with default DNS.", "无法解析节点 IP，继续使用默认 DNS。"))
		return Endpoint{}
	}

	endpoints := make([]Endpoint, 0, len(ips))
	for _, ip := range ips {
		desc := fetchIPDescFn(ctx, ip)
		endpoints = append(endpoints, Endpoint{IP: ip, Desc: desc})
	}

	bus.Info(i18n.Text("Available endpoints:", "可用节点:"))
	for i, ep := range endpoints {
		bus.Info(fmt.Sprintf("  %d) %s  %s", i+1, ep.IP, ep.Desc))
	}

	choice := 0
	if len(endpoints) > 1 && isTTY {
		// Ensure all queued endpoint lines are rendered before interactive prompt.
		bus.Flush()
		var cancelled bool
		choice, cancelled = promptChoice(ctx, len(endpoints), bus)
		if cancelled {
			// Don't log here; runner.go checks ctx.Err() and logs "Interrupted" once.
			return Endpoint{}
		}
	}
	selected := endpoints[choice]
	bus.Info(fmt.Sprintf(i18n.Text("Selected endpoint: %s (%s)", "已选择节点: %s (%s)"), selected.IP, selected.Desc))
	return selected
}

func HostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

type dohResponse struct {
	Answer []struct {
		Data string `json:"data"`
	} `json:"Answer"`
}

// resolveDoHDual concurrently queries both CF and Ali DoH providers for A
// and AAAA records (four queries total). It returns the merged IP list in
// the order CF-A, CF-AAAA, Ali-A, Ali-AAAA (deduplicated) and each
// provider's timeout status. A provider is considered timed-out only when
// both its A and AAAA queries timed out.
func resolveDoHDual(ctx context.Context, host string) ([]string, bool, bool) {
	var wg sync.WaitGroup
	wg.Add(4)

	var cfARes, cfAAAARes, aliARes, aliAAAARes dohResult

	// Cloudflare DoH A
	go func() {
		defer wg.Done()
		cfARes = queryCFDoH(ctx, host, cfDoHURLTemplate)
	}()

	// Cloudflare DoH AAAA
	go func() {
		defer wg.Done()
		cfAAAARes = queryCFDoH(ctx, host, cfDoHAAAAURLTemplate)
	}()

	// AliDNS DoH A
	go func() {
		defer wg.Done()
		aliARes = queryAliDoH(ctx, host, aliDoHURLTemplate)
	}()

	// AliDNS DoH AAAA
	go func() {
		defer wg.Done()
		aliAAAARes = queryAliDoH(ctx, host, aliDoHAAAAURLTemplate)
	}()

	wg.Wait()

	// Merge order: CF-A, CF-AAAA, Ali-A, Ali-AAAA (deduplicated)
	merged := mergeIPs4(cfARes.ips, cfAAAARes.ips, aliARes.ips, aliAAAARes.ips)
	cfTimedOut := cfARes.timedOut && cfAAAARes.timedOut
	aliTimedOut := aliARes.timedOut && aliAAAARes.timedOut
	return merged, cfTimedOut, aliTimedOut
}

// queryCFDoH queries Cloudflare DoH (application/dns-json format).
func queryCFDoH(ctx context.Context, host string, urlTemplate string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(urlTemplate, host)
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return dohResult{err: err}
	}
	req.Header.Set("Accept", "application/dns-json")

	resp, err := dohHTTPClient.Do(req)
	if err != nil {
		return dohResult{timedOut: isTimeoutErr(err), err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dohResult{err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return dohResult{timedOut: isTimeoutErr(err), err: err}
	}
	ips := extractIPsFromBody(body)
	return dohResult{ips: ips}
}

// queryAliDoH queries AliDNS DoH (short=1 format).
func queryAliDoH(ctx context.Context, host string, urlTemplate string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(urlTemplate, host)
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return dohResult{err: err}
	}

	resp, err := dohHTTPClient.Do(req)
	if err != nil {
		return dohResult{timedOut: isTimeoutErr(err), err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dohResult{err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return dohResult{timedOut: isTimeoutErr(err), err: err}
	}
	ips := extractIPsFromBody(body)
	return dohResult{ips: ips}
}

// extractIPsFromBody tries JSON structured parsing first, then falls back to
// regex extraction. Returns deduplicated IP addresses (IPv4 and/or IPv6)
// preserving order.
func extractIPsFromBody(body []byte) []string {
	// Try structured JSON first
	var dr dohResponse
	if json.Unmarshal(body, &dr) == nil && len(dr.Answer) > 0 {
		seen := map[string]bool{}
		var out []string
		for _, a := range dr.Answer {
			ip := strings.TrimSpace(a.Data)
			if net.ParseIP(ip) != nil && !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	// Regex fallback: find all IPv4 and IPv6 matches by position to
	// preserve the order they appear in the response body.
	s := string(body)
	type match struct {
		pos int
		ip  string
	}
	var matches []match
	for _, loc := range ipv4Re.FindAllStringIndex(s, -1) {
		matches = append(matches, match{pos: loc[0], ip: s[loc[0]:loc[1]]})
	}
	for _, loc := range ipv6Re.FindAllStringIndex(s, -1) {
		matches = append(matches, match{pos: loc[0], ip: s[loc[0]:loc[1]]})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pos < matches[j].pos })
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if net.ParseIP(m.ip) == nil {
			continue
		}
		if !seen[m.ip] {
			seen[m.ip] = true
			out = append(out, m.ip)
		}
	}
	return out
}

// mergeIPs merges two IP slices (first before second) and deduplicates.
func mergeIPs(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range first {
		if net.ParseIP(ip) == nil {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	for _, ip := range second {
		if net.ParseIP(ip) == nil {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

// mergeIPs4 merges four IP slices in order and deduplicates.
func mergeIPs4(a, b, c, d []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b, c, d} {
		for _, ip := range list {
			if net.ParseIP(ip) == nil {
				continue
			}
			if !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
	}
	return out
}

// isTimeoutErr checks whether an error is a timeout (context deadline exceeded
// or net.Error timeout).
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	// Also check wrapped errors
	if ue, ok := err.(*url.Error); ok {
		return isTimeoutErr(ue.Err)
	}
	return false
}

// ResolveHost tries system DNS and returns the first IPv4 address, or "".
func ResolveHost(host string) string {
	return resolveSystem(host)
}

func resolveSystem(host string) string {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if net.ParseIP(a) != nil && strings.Contains(a, ".") {
			return a
		}
	}
	return ""
}

func fetchIPDesc(ctx context.Context, ip string) string {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return i18n.Text("lookup failed", "查询失败")
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		desc, err := doFetchIPDesc(ctx, ip)
		if err != nil {
			continue
		}
		return desc
	}
	return i18n.Text("lookup failed", "查询失败")
}

// ipAPILangSuffix returns "&lang=zh-CN" when the UI language is Chinese,
// otherwise an empty string (ip-api defaults to English).
func ipAPILangSuffix() string {
	if i18n.IsZH() {
		return "&lang=zh-CN"
	}
	return ""
}

// buildIPAPIURL constructs an ip-api JSON endpoint URL with the given target
// (empty string for self-lookup) and fields, appending the language suffix
// when in Chinese mode.
func buildIPAPIURL(target, fields string) string {
	if target == "" {
		return fmt.Sprintf("http://ip-api.com/json/?fields=%s%s", fields, ipAPILangSuffix())
	}
	return fmt.Sprintf("http://ip-api.com/json/%s?fields=%s%s", target, fields, ipAPILangSuffix())
}

func doFetchIPDesc(ctx context.Context, ip string) (string, error) {
	ctx2, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	reqURL := buildIPAPIURL(ip, "status,city,regionName,country,as,org")
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var info IPInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	if info.Status != "success" {
		return "", fmt.Errorf("ip-api status: %s", info.Status)
	}

	loc := info.City
	if info.RegionName != "" && info.RegionName != info.City {
		loc += ", " + info.RegionName
	}
	if info.Country != "" {
		loc += ", " + info.Country
	}
	if loc == "" {
		loc = i18n.Text("unknown location", "未知位置")
	}
	asn := info.AS
	if asn == "" {
		asn = info.Org
	}
	if asn != "" {
		loc += " (" + asn + ")"
	}
	return loc, nil
}

func FetchInfo(ctx context.Context, target string) IPInfo {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return IPInfo{}
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		info, err := doFetchInfo(ctx, target)
		if err != nil {
			continue
		}
		return info
	}
	return IPInfo{}
}

func doFetchInfo(ctx context.Context, target string) (IPInfo, error) {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var reqURL string
	if target == "" {
		reqURL = buildIPAPIURL("", "status,query,as,isp,city,regionName,country")
	} else {
		reqURL = buildIPAPIURL(target, "status,query,as,isp,org,city,regionName,country")
	}
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return IPInfo{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return IPInfo{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var info IPInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return IPInfo{}, err
	}
	if info.Status != "" && info.Status != "success" {
		return IPInfo{}, fmt.Errorf("ip-api status: %s", info.Status)
	}
	return info, nil
}

// promptChoice displays an interactive prompt and waits for user input.
// It returns (choiceIndex, cancelled). When ctx is cancelled (e.g. Ctrl+C),
// the tty is closed to unblock the read and cancelled=true is returned.
func promptChoice(ctx context.Context, count int, bus *render.Bus) (int, bool) {
	fmt.Fprintf(os.Stderr, "  \033[36m\033[1m[?]\033[0m %s", fmt.Sprintf(i18n.Text("Select endpoint [1-%d, Enter=1]: ", "选择节点 [1-%d，回车=1]: "), count))

	tty, shouldClose, err := openPromptInputFn()
	if err != nil {
		bus.Warn(i18n.Text("Interactive input unavailable, defaulting to endpoint 1.", "交互输入不可用，默认使用节点 1。"))
		return 0, false
	}

	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)

	go func() {
		reader := bufio.NewReader(tty)
		line, err := reader.ReadString('\n')
		ch <- readResult{line, err}
	}()

	select {
	case <-ctx.Done():
		// Context cancelled (e.g. Ctrl+C). Close the tty to try to
		// unblock the goroutine. We do NOT wait for the goroutine:
		// on some OSes (macOS) closing a tty won't unblock a concurrent
		// read, and the process is about to exit anyway.
		if shouldClose {
			tty.Close()
		}
		return 0, true
	case res := <-ch:
		if shouldClose {
			tty.Close()
		}
		if res.err != nil && res.line == "" {
			// EOF or read error with no data — treat as default
			return 0, false
		}
		choice, ok := parseChoice(res.line, count)
		if !ok {
			line := strings.TrimSpace(res.line)
			bus.Warn(fmt.Sprintf(i18n.Text("Invalid selection '%s', fallback to 1.", "选择无效 '%s'，回退到 1。"), line))
			return 0, false
		}
		return choice, false
	}
}

func openPromptInput() (*os.File, bool, error) {
	for _, p := range []string{"/dev/tty", "CONIN$"} {
		f, err := os.Open(p)
		if err == nil {
			return f, true, nil
		}
	}

	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stdin, false, nil
	}
	return nil, false, fmt.Errorf("interactive input not available")
}

func parseChoice(line string, count int) (int, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, true
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > count {
		return 0, false
	}
	return n - 1, true
}
