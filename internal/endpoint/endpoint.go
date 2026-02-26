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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

var (
	cfDoHURLTemplate  = "https://cloudflare-dns.com/dns-query?name=%s&type=A"
	aliDoHURLTemplate = "https://dns.alidns.com/resolve?name=%s&type=A&short=1"

	// dohTimeout is the per-provider timeout for DoH queries.
	dohTimeout = 1 * time.Second

	dohHTTPClient   = http.DefaultClient
	resolveDoHFn    = resolveDoHDual
	resolveSystemFn = resolveSystem
	fetchIPDescFn   = fetchIPDesc
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
		bus.Warn(i18n.Text("Dual DoH returned no IPv4 endpoint, continue with default DNS.", "双 DoH 未返回 IPv4 节点，继续使用默认 DNS。"))
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
			bus.Warn(i18n.Text("Interrupted.", "已中断。"))
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

// resolveDoHDual concurrently queries both CF and Ali DoH providers.
// It returns the merged (CF first, Ali second, deduplicated) IP list and
// each provider's timeout status.
func resolveDoHDual(ctx context.Context, host string) ([]string, bool, bool) {
	var wg sync.WaitGroup
	wg.Add(2)

	var cfRes, aliRes dohResult

	// Cloudflare DoH
	go func() {
		defer wg.Done()
		cfRes = queryCFDoH(ctx, host)
	}()

	// AliDNS DoH
	go func() {
		defer wg.Done()
		aliRes = queryAliDoH(ctx, host)
	}()

	wg.Wait()

	// Merge: CF first, Ali second, deduplicated
	merged := mergeIPs(cfRes.ips, aliRes.ips)
	return merged, cfRes.timedOut, aliRes.timedOut
}

// queryCFDoH queries Cloudflare DoH (application/dns-json format).
func queryCFDoH(ctx context.Context, host string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(cfDoHURLTemplate, host)
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
func queryAliDoH(ctx context.Context, host string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(aliDoHURLTemplate, host)
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
// regex extraction. Returns deduplicated IPv4 addresses preserving order.
func extractIPsFromBody(body []byte) []string {
	// Try structured JSON first
	var dr dohResponse
	if json.Unmarshal(body, &dr) == nil && len(dr.Answer) > 0 {
		seen := map[string]bool{}
		var out []string
		for _, a := range dr.Answer {
			ip := strings.TrimSpace(a.Data)
			parsed := net.ParseIP(ip)
			if parsed != nil && parsed.To4() != nil && !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	// Regex fallback
	all := ipv4Re.FindAllString(string(body), -1)
	seen := map[string]bool{}
	var out []string
	for _, ip := range all {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

// mergeIPs merges two IP slices (first before second) and deduplicates.
func mergeIPs(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range first {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	for _, ip := range second {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
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

func doFetchIPDesc(ctx context.Context, ip string) (string, error) {
	ctx2, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	reqURL := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,city,regionName,country,as,org", ip)
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
		reqURL = "http://ip-api.com/json/?fields=status,query,as,isp,city,regionName,country"
	} else {
		reqURL = fmt.Sprintf("http://ip-api.com/json/%s?fields=status,query,as,isp,org,city,regionName,country", target)
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

	tty, shouldClose, err := openPromptInput()
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
