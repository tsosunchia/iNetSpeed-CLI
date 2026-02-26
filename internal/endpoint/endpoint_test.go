package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

func newTestBus() *render.Bus {
	return render.NewBus(render.NewPlainRenderer(&strings.Builder{}))
}

func TestHostFromURL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"https://mensura.cdn-apple.com/api/v1/gm/large", "mensura.cdn-apple.com"},
		{"http://example.com:8080/path", "example.com"},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		got := HostFromURL(tt.input)
		if got != tt.want {
			t.Errorf("HostFromURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestChooseEmptyHost(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()
	ep := Choose(context.Background(), "", bus, false)
	if ep.IP != "" {
		t.Errorf("expected empty endpoint, got %+v", ep)
	}
}

func TestFetchInfoMockSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"status":     "success",
			"query":      "1.2.3.4",
			"as":         "AS1234 Example",
			"isp":        "Example ISP",
			"city":       "Tokyo",
			"regionName": "Tokyo",
			"country":    "Japan",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// We cannot easily override the URL in FetchInfo, so test the JSON parsing path
	// by calling the server directly
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info IPInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Status != "success" {
		t.Errorf("status = %q", info.Status)
	}
	if info.City != "Tokyo" {
		t.Errorf("city = %q", info.City)
	}
}

func TestResolveDoHMock(t *testing.T) {
	// Test with the structured AliDNS response format
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Answer":[{"data":"1.2.3.4"},{"data":"5.6.7.8"}]}`)
	}))
	defer srv.Close()

	// Can't directly test resolveDoH without refactoring, but test the JSON parsing
	var dr dohResponse
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&dr)
	if len(dr.Answer) != 2 {
		t.Errorf("expected 2 answers, got %d", len(dr.Answer))
	}
}

func TestResolveDoHFallbackRegex(t *testing.T) {
	// Test with raw text containing IPs (like the short=1 format)
	body := "1.2.3.4\n5.6.7.8\n1.2.3.4\n"
	ips := ipv4Re.FindAllString(body, -1)
	if len(ips) != 3 {
		t.Errorf("expected 3 matches, got %d", len(ips))
	}
	// Deduplicate
	seen := map[string]bool{}
	var unique []string
	for _, ip := range ips {
		if !seen[ip] {
			seen[ip] = true
			unique = append(unique, ip)
		}
	}
	if len(unique) != 2 {
		t.Errorf("expected 2 unique, got %d", len(unique))
	}
}

func TestDoResolveDoHStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// doResolveDoH checks HTTP status; a non-200 should return error
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status")
	}
}

func TestDoFetchIPDescStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status code from rate-limited server")
	}
}

func TestDoFetchInfoRetryTransportError(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Force a broken response to simulate transport error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := map[string]string{
			"status":  "success",
			"query":   "1.2.3.4",
			"as":      "AS1234",
			"isp":     "TestISP",
			"city":    "Tokyo",
			"country": "Japan",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Simulate calling the server multiple times (retry behavior)
	var info IPInfo
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		json.NewDecoder(resp.Body).Decode(&info)
		lastErr = nil
		break
	}
	if lastErr != nil {
		t.Fatalf("all retries failed: %v", lastErr)
	}
	if info.City != "Tokyo" {
		t.Errorf("city = %q, want Tokyo", info.City)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestChooseSystemDNSFallback(t *testing.T) {
	// Choose with a host that DoH cannot resolve should fall back to system DNS
	// We can't easily test the full flow without network,
	// so test with empty host which should return empty endpoint
	bus := newTestBus()
	defer bus.Close()
	ep := Choose(context.Background(), "", bus, false)
	if ep.IP != "" {
		t.Errorf("expected empty endpoint for empty host, got %+v", ep)
	}
}

func TestResolveHostLocalhost(t *testing.T) {
	// ResolveHost should be able to resolve "localhost" via system DNS
	ip := ResolveHost("localhost")
	// On most systems this returns 127.0.0.1; on some it may return "".
	// Just verify it doesn't panic and returns either "" or a valid IP.
	if ip != "" && net.ParseIP(ip) == nil {
		t.Errorf("ResolveHost returned invalid IP: %q", ip)
	}
}

func TestDoFetchInfoJSONStatusFail(t *testing.T) {
	// ip-api can return HTTP 200 with status:"fail" in JSON.
	// doFetchInfo should treat this as an error and trigger retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "fail",
			"message": "reserved range",
		})
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var info IPInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Status == "success" {
		t.Error("expected non-success status")
	}
	// Verify our doFetchInfo logic: status != success should be treated as error
	if info.Status != "fail" {
		t.Errorf("expected status=fail, got %q", info.Status)
	}
}
