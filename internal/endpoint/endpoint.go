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

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/netx"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var ipv6Re = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}`)

var (
	cfDoHURLTemplate      = "https://cloudflare-dns.com/dns-query?name=%s&type=A"
	cfDoHAAAAURLTemplate  = "https://cloudflare-dns.com/dns-query?name=%s&type=AAAA"
	aliDoHURLTemplate     = "https://dns.alidns.com/resolve?name=%s&type=A&short=1"
	aliDoHAAAAURLTemplate = "https://dns.alidns.com/resolve?name=%s&type=AAAA&short=1"

	dohTimeout         = 1 * time.Second
	dohHTTPClient      = netx.NewClient(netx.Options{Timeout: 4 * time.Second})
	metadataHTTPClient = netx.NewClient(netx.Options{Timeout: 5 * time.Second})
	resolveDoHFn       = resolveDoHDual
	resolveSystemFn    = resolveSystem
	fetchIPDescFn      = fetchIPDesc
	fetchInfoFn        = fetchInfo
	probeEndpointFn    = probeEndpoint
	openPromptInputFn  = openPromptInput
)

type Endpoint struct {
	IP     string
	Desc   string
	RTTMs  float64
	Source string
	Status string
}

type Candidate struct {
	IP     string
	Desc   string
	RTTMs  float64
	Source string
	Status string
	Error  string
}

type Warning struct {
	Code    string
	Message string
}

type DiscoveryOptions struct {
	ProbeURL   string
	EndpointIP string
	Metadata   bool
}

type DiscoveryResult struct {
	Host       string
	Candidates []Candidate
	Selected   Endpoint
	Warnings   []Warning
	DefaultDNS bool
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

type dohResult struct {
	ips      []string
	timedOut bool
	err      error
}

type dohResponse struct {
	Answer []struct {
		Data string `json:"data"`
	} `json:"Answer"`
}

func Discover(ctx context.Context, host string, opts DiscoveryOptions) DiscoveryResult {
	res := DiscoveryResult{Host: host}
	if host == "" {
		res.DefaultDNS = true
		res.Warnings = append(res.Warnings, Warning{
			Code:    "host_unavailable",
			Message: i18n.Text("Could not parse host from URL. Continue with default DNS.", "无法从 URL 解析主机，继续使用默认 DNS。"),
		})
		res.Selected = Endpoint{Source: "default_dns", Status: "degraded"}
		return res
	}

	if opts.EndpointIP != "" {
		candidate := buildCandidate(ctx, host, opts.EndpointIP, "user", opts)
		res.Candidates = []Candidate{candidate}
		res.Selected = endpointFromCandidate(candidate)
		if candidate.Error != "" {
			res.Warnings = append(res.Warnings, Warning{
				Code:    "endpoint_probe_failed",
				Message: i18n.Text("Forced endpoint probe failed; continuing with the requested IP.", "指定节点探测失败，继续使用用户指定 IP。"),
			})
		}
		return res
	}

	ips, cfTimedOut, aliTimedOut := resolveDoHFn(ctx, host)
	if len(ips) > 0 {
		original := make([]Candidate, 0, len(ips))
		for _, ip := range ips {
			original = append(original, buildCandidate(ctx, host, ip, "doh", opts))
		}
		res.Candidates = orderCandidates(original)
		res.Selected = chooseAuto(original, res.Candidates)
		if res.Selected.IP == "" {
			res.Selected = Endpoint{Source: "default_dns", Status: "degraded"}
			res.DefaultDNS = true
			res.Warnings = append(res.Warnings, Warning{
				Code:    "default_dns_fallback",
				Message: i18n.Text("No healthy endpoint candidate. Continue with default DNS.", "没有健康的候选节点，继续使用默认 DNS。"),
			})
		}
		return res
	}

	if cfTimedOut && aliTimedOut {
		res.Warnings = append(res.Warnings, Warning{
			Code:    "system_dns_fallback",
			Message: i18n.Text("Dual DoH (CF + Ali) both timed out. Fallback to system DNS.", "双 DoH（CF + Ali）均超时，回退系统 DNS。"),
		})
		if ip := resolveSystemFn(host); ip != "" {
			candidate := buildCandidate(ctx, host, ip, "system_dns", opts)
			res.Candidates = []Candidate{candidate}
			res.Selected = endpointFromCandidate(candidate)
			return res
		}
	}

	res.DefaultDNS = true
	res.Selected = Endpoint{Source: "default_dns", Status: "degraded"}
	res.Warnings = append(res.Warnings, Warning{
		Code:    "default_dns_fallback",
		Message: i18n.Text("Could not resolve endpoint IP. Continue with default DNS.", "无法解析节点 IP，继续使用默认 DNS。"),
	})
	return res
}

func Choose(ctx context.Context, host string, bus *render.Bus, isTTY bool) Endpoint {
	bus.Header(i18n.Text("Endpoint Selection", "节点选择"))
	res := Discover(ctx, host, DiscoveryOptions{Metadata: true})
	if host != "" {
		bus.Info(i18n.Text("Host: ", "主机: ") + host)
	}
	for _, warning := range res.Warnings {
		bus.Warn(warning.Message)
	}
	if len(res.Candidates) == 0 {
		return res.Selected
	}

	bus.Info(i18n.Text("Available endpoints:", "可用节点:"))
	for i, candidate := range res.Candidates {
		bus.Info(fmt.Sprintf("  %d) %s  %s", i+1, candidate.IP, candidateLabel(candidate)))
	}

	selected := res.Selected
	if len(res.Candidates) > 1 && isTTY {
		bus.Flush()
		choice, cancelled := promptChoice(ctx, len(res.Candidates), bus)
		if cancelled {
			return Endpoint{}
		}
		selected = endpointFromCandidate(res.Candidates[choice])
	}
	if selected.IP != "" {
		bus.Info(fmt.Sprintf(i18n.Text("Selected endpoint: %s (%s)", "已选择节点: %s (%s)"), selected.IP, selected.Desc))
	}
	return selected
}

func HostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func ResolveHost(host string) string {
	return resolveSystem(host)
}

func FetchInfo(ctx context.Context, target string) IPInfo {
	return fetchInfoFn(ctx, target)
}

func PromptChoice(ctx context.Context, count int, bus *render.Bus) (int, bool) {
	return promptChoice(ctx, count, bus)
}

func CandidateLabel(candidate Candidate) string {
	return candidateLabel(candidate)
}

func resolveDoHDual(ctx context.Context, host string) ([]string, bool, bool) {
	var wg sync.WaitGroup
	wg.Add(4)

	var cfARes, cfAAAARes, aliARes, aliAAAARes dohResult

	go func() {
		defer wg.Done()
		cfARes = queryCFDoH(ctx, host, cfDoHURLTemplate)
	}()
	go func() {
		defer wg.Done()
		cfAAAARes = queryCFDoH(ctx, host, cfDoHAAAAURLTemplate)
	}()
	go func() {
		defer wg.Done()
		aliARes = queryAliDoH(ctx, host, aliDoHURLTemplate)
	}()
	go func() {
		defer wg.Done()
		aliAAAARes = queryAliDoH(ctx, host, aliDoHAAAAURLTemplate)
	}()

	wg.Wait()

	merged := mergeIPs4(cfARes.ips, cfAAAARes.ips, aliARes.ips, aliAAAARes.ips)
	cfTimedOut := cfARes.timedOut && cfAAAARes.timedOut
	aliTimedOut := aliARes.timedOut && aliAAAARes.timedOut
	return merged, cfTimedOut, aliTimedOut
}

func queryCFDoH(ctx context.Context, host string, urlTemplate string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(urlTemplate, host)
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return dohResult{err: err}
	}
	req.Header.Set("Accept", "application/dns-json")
	req.Header.Set("User-Agent", "iNetSpeed-CLI")

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
	return dohResult{ips: extractIPsFromBody(body)}
}

func queryAliDoH(ctx context.Context, host string, urlTemplate string) dohResult {
	ctx2, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	reqURL := fmt.Sprintf(urlTemplate, host)
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return dohResult{err: err}
	}
	req.Header.Set("User-Agent", "iNetSpeed-CLI")

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
	return dohResult{ips: extractIPsFromBody(body)}
}

func extractIPsFromBody(body []byte) []string {
	var dr dohResponse
	if json.Unmarshal(body, &dr) == nil && len(dr.Answer) > 0 {
		seen := map[string]bool{}
		var out []string
		for _, answer := range dr.Answer {
			ip := strings.TrimSpace(answer.Data)
			if net.ParseIP(ip) != nil && !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	text := string(body)
	type match struct {
		pos int
		ip  string
	}
	var matches []match
	for _, loc := range ipv4Re.FindAllStringIndex(text, -1) {
		matches = append(matches, match{pos: loc[0], ip: text[loc[0]:loc[1]]})
	}
	for _, loc := range ipv6Re.FindAllStringIndex(text, -1) {
		matches = append(matches, match{pos: loc[0], ip: text[loc[0]:loc[1]]})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pos < matches[j].pos })
	seen := map[string]bool{}
	var out []string
	for _, match := range matches {
		if net.ParseIP(match.ip) == nil || seen[match.ip] {
			continue
		}
		seen[match.ip] = true
		out = append(out, match.ip)
	}
	return out
}

func mergeIPs(first, second []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{first, second} {
		for _, ip := range list {
			if net.ParseIP(ip) == nil || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

func mergeIPs4(a, b, c, d []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b, c, d} {
		for _, ip := range list {
			if net.ParseIP(ip) == nil || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

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
	if ue, ok := err.(*url.Error); ok {
		return isTimeoutErr(ue.Err)
	}
	return false
}

func resolveSystem(host string) string {
	addrs, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	for _, ip := range addrs {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	for _, ip := range addrs {
		if ip != nil {
			return ip.String()
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
		if err == nil {
			return desc
		}
	}
	return i18n.Text("lookup failed", "查询失败")
}

func fetchInfo(ctx context.Context, target string) IPInfo {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return IPInfo{}
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		info, err := doFetchInfo(ctx, target)
		if err == nil {
			return info
		}
	}
	return IPInfo{}
}

func ipAPILangSuffix() string {
	if i18n.IsZH() {
		return "&lang=zh-CN"
	}
	return ""
}

func buildIPAPIURL(target, fields string) string {
	if target == "" {
		return fmt.Sprintf("http://ip-api.com/json/?fields=%s%s", fields, ipAPILangSuffix())
	}
	return fmt.Sprintf("http://ip-api.com/json/%s?fields=%s%s", target, fields, ipAPILangSuffix())
}

func doFetchIPDesc(ctx context.Context, ip string) (string, error) {
	ctx2, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, buildIPAPIURL(ip, "status,city,regionName,country,as,org"), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "iNetSpeed-CLI")
	resp, err := metadataHTTPClient.Do(req)
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
		if loc != "" {
			loc += ", "
		}
		loc += info.RegionName
	}
	if info.Country != "" {
		if loc != "" {
			loc += ", "
		}
		loc += info.Country
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

func doFetchInfo(ctx context.Context, target string) (IPInfo, error) {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	fields := "status,query,as,isp,city,regionName,country"
	if target != "" {
		fields = "status,query,as,isp,org,city,regionName,country"
	}
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, buildIPAPIURL(target, fields), nil)
	if err != nil {
		return IPInfo{}, err
	}
	req.Header.Set("User-Agent", "iNetSpeed-CLI")
	resp, err := metadataHTTPClient.Do(req)
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

func probeEndpoint(ctx context.Context, host, probeURL, ip string) (float64, error) {
	if host == "" || probeURL == "" || ip == "" {
		return 0, fmt.Errorf("probe unavailable")
	}

	client := netx.NewClient(netx.Options{
		PinHost: host,
		PinIP:   ip,
		Timeout: 3 * time.Second,
	})
	ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", config.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}
	return float64(time.Since(start).Microseconds()) / 1000.0, nil
}

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
		ch <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		if shouldClose {
			tty.Close()
		}
		return 0, true
	case result := <-ch:
		if shouldClose {
			tty.Close()
		}
		if result.err != nil && result.line == "" {
			return 0, false
		}
		choice, ok := parseChoice(result.line, count)
		if !ok {
			line := strings.TrimSpace(result.line)
			bus.Warn(fmt.Sprintf(i18n.Text("Invalid selection '%s', fallback to 1.", "选择无效 '%s'，回退到 1。"), line))
			return 0, false
		}
		return choice, false
	}
}

func openPromptInput() (*os.File, bool, error) {
	for _, path := range []string{"/dev/tty", "CONIN$"} {
		file, err := os.Open(path)
		if err == nil {
			return file, true, nil
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

func buildCandidate(ctx context.Context, host, ip, source string, opts DiscoveryOptions) Candidate {
	candidate := Candidate{
		IP:     ip,
		Source: source,
		Status: "degraded",
	}
	if opts.Metadata {
		candidate.Desc = fetchIPDescFn(ctx, ip)
	}
	if opts.ProbeURL == "" {
		return candidate
	}
	rtt, err := probeEndpointFn(ctx, host, opts.ProbeURL, ip)
	if err != nil {
		candidate.Error = err.Error()
		return candidate
	}
	candidate.RTTMs = rtt
	candidate.Status = "ok"
	return candidate
}

func chooseAuto(original, ordered []Candidate) Endpoint {
	for _, candidate := range ordered {
		if candidate.Status == "ok" {
			return endpointFromCandidate(candidate)
		}
	}
	for _, candidate := range original {
		if candidate.IP != "" {
			return endpointFromCandidate(candidate)
		}
	}
	return Endpoint{}
}

func orderCandidates(candidates []Candidate) []Candidate {
	ordered := append([]Candidate(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftOK := ordered[i].Status == "ok"
		rightOK := ordered[j].Status == "ok"
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && ordered[i].RTTMs != ordered[j].RTTMs {
			return ordered[i].RTTMs < ordered[j].RTTMs
		}
		return false
	})
	return ordered
}

func endpointFromCandidate(candidate Candidate) Endpoint {
	return Endpoint{
		IP:     candidate.IP,
		Desc:   candidate.Desc,
		RTTMs:  candidate.RTTMs,
		Source: candidate.Source,
		Status: candidate.Status,
	}
}

func candidateLabel(candidate Candidate) string {
	parts := []string{}
	if candidate.Desc != "" {
		parts = append(parts, candidate.Desc)
	}
	if candidate.RTTMs > 0 {
		parts = append(parts, fmt.Sprintf("%.2f ms", candidate.RTTMs))
	} else if candidate.Error != "" {
		parts = append(parts, i18n.Text("probe unavailable", "探测不可用"))
	}
	if len(parts) == 0 {
		return i18n.Text("unavailable", "不可用")
	}
	return strings.Join(parts, "  ")
}
