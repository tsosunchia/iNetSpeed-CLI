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
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

var ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

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

func Choose(ctx context.Context, host string, bus *render.Bus, isTTY bool) Endpoint {
	bus.Header("Endpoint Selection")
	if host == "" {
		bus.Warn("Could not parse host from DL_URL. Skip endpoint selection.")
		return Endpoint{}
	}
	bus.Info("Host: " + host)

	ips := resolveDoH(ctx, host)
	if len(ips) == 0 {
		bus.Warn("AliDNS DoH returned no IPv4 endpoint. Fallback to system DNS.")
		fb := resolveSystem(host)
		if fb != "" {
			ep := Endpoint{IP: fb, Desc: "system DNS fallback"}
			bus.Info("Selected endpoint: " + ep.IP + " (" + ep.Desc + ")")
			return ep
		}
		bus.Warn("Could not resolve endpoint IP, continue with default DNS.")
		return Endpoint{}
	}

	endpoints := make([]Endpoint, 0, len(ips))
	for _, ip := range ips {
		desc := fetchIPDesc(ctx, ip)
		endpoints = append(endpoints, Endpoint{IP: ip, Desc: desc})
	}

	bus.Info("Available endpoints:")
	for i, ep := range endpoints {
		bus.Info(fmt.Sprintf("  %d) %s  %s", i+1, ep.IP, ep.Desc))
	}

	choice := 0
	if len(endpoints) > 1 && isTTY {
		choice = promptChoice(len(endpoints), bus)
	}
	selected := endpoints[choice]
	bus.Info(fmt.Sprintf("Selected endpoint: %s (%s)", selected.IP, selected.Desc))
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

func resolveDoH(ctx context.Context, host string) []string {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		ips, err := doResolveDoH(ctx, host)
		if err != nil {
			continue
		}
		return ips
	}
	return nil
}

func doResolveDoH(ctx context.Context, host string) ([]string, error) {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reqURL := fmt.Sprintf("https://dns.alidns.com/resolve?name=%s&type=A&short=1", host)
	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

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
			return out, nil
		}
	}

	all := ipv4Re.FindAllString(string(body), -1)
	seen := map[string]bool{}
	var out []string
	for _, ip := range all {
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	return nil, fmt.Errorf("no IPs found in DoH response")
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
				return "lookup failed"
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		desc, err := doFetchIPDesc(ctx, ip)
		if err != nil {
			continue
		}
		return desc
	}
	return "lookup failed"
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
		loc = "unknown location"
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

func promptChoice(count int, bus *render.Bus) int {
	fmt.Fprintf(os.Stderr, "  \033[36m\033[1m[?]\033[0m Select endpoint [1-%d, Enter=1]: ", count)

	tty, err := os.Open("/dev/tty")
	if err != nil {
		bus.Warn("/dev/tty unavailable, defaulting to endpoint 1.")
		return 0
	}
	defer tty.Close()
	reader := bufio.NewReader(tty)

	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return 0
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > count {
		bus.Warn(fmt.Sprintf("Invalid selection '%s', fallback to 1.", line))
		return 0
	}
	return n - 1
}
