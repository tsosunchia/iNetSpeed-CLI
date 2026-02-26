package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestResolveDoHFallbackRegex(t *testing.T) {
	body := "1.2.3.4\n5.6.7.8\n1.2.3.4\n"
	ips := ipv4Re.FindAllString(body, -1)
	if len(ips) != 3 {
		t.Errorf("expected 3 matches, got %d", len(ips))
	}
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
	oldResolveDoH := resolveDoHFn
	oldResolveSystem := resolveSystemFn
	t.Cleanup(func() {
		resolveDoHFn = oldResolveDoH
		resolveSystemFn = oldResolveSystem
	})

	resolveDoHFn = func(ctx context.Context, host string) ([]string, bool, bool) {
		return nil, true, true
	}
	resolveSystemFn = func(host string) string {
		return "9.9.9.9"
	}

	bus := newTestBus()
	defer bus.Close()
	ep := Choose(context.Background(), "mensura.cdn-apple.com", bus, false)
	if ep.IP != "9.9.9.9" {
		t.Errorf("expected system fallback IP, got %+v", ep)
	}
	if ep.Desc != "system DNS fallback" {
		t.Errorf("expected fallback desc, got %q", ep.Desc)
	}
}

func TestChooseNoFallbackWhenDualDoHNoIPs(t *testing.T) {
	oldResolveDoH := resolveDoHFn
	oldResolveSystem := resolveSystemFn
	t.Cleanup(func() {
		resolveDoHFn = oldResolveDoH
		resolveSystemFn = oldResolveSystem
	})

	resolveDoHFn = func(ctx context.Context, host string) ([]string, bool, bool) {
		return nil, false, false
	}
	resolveSystemCalled := false
	resolveSystemFn = func(host string) string {
		resolveSystemCalled = true
		return "8.8.8.8"
	}

	bus := newTestBus()
	defer bus.Close()
	ep := Choose(context.Background(), "mensura.cdn-apple.com", bus, false)
	if ep.IP != "" {
		t.Errorf("expected empty endpoint when dual DoH has no IPs but no timeout, got %+v", ep)
	}
	if resolveSystemCalled {
		t.Error("expected system DNS not to be called")
	}
}

func TestResolveHostLocalhost(t *testing.T) {
	ip := ResolveHost("localhost")
	if ip != "" && net.ParseIP(ip) == nil {
		t.Errorf("ResolveHost returned invalid IP: %q", ip)
	}
}

func TestDoFetchInfoJSONStatusFail(t *testing.T) {
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
	if info.Status != "fail" {
		t.Errorf("expected status=fail, got %q", info.Status)
	}
}

func TestParseChoice(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		count int
		want  int
		ok    bool
	}{
		{name: "empty defaults", line: "", count: 4, want: 0, ok: true},
		{name: "newline defaults", line: "\n", count: 4, want: 0, ok: true},
		{name: "valid one", line: "1", count: 4, want: 0, ok: true},
		{name: "valid with spaces", line: " 3 ", count: 4, want: 2, ok: true},
		{name: "zero invalid", line: "0", count: 4, want: 0, ok: false},
		{name: "out of range invalid", line: "5", count: 4, want: 0, ok: false},
		{name: "non number invalid", line: "abc", count: 4, want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseChoice(tt.line, tt.count)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseChoice(%q, %d) = (%d, %v), want (%d, %v)", tt.line, tt.count, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// ---------------------------------------------------------------------------
//  Dual DoH unit tests
// ---------------------------------------------------------------------------

func TestExtractIPsFromBody_JSON(t *testing.T) {
	body := []byte(`{"Answer":[{"data":"1.2.3.4"},{"data":"5.6.7.8"},{"data":"1.2.3.4"}]}`)
	ips := extractIPsFromBody(body)
	if len(ips) != 2 {
		t.Fatalf("expected 2 unique IPs, got %d: %v", len(ips), ips)
	}
	if ips[0] != "1.2.3.4" || ips[1] != "5.6.7.8" {
		t.Errorf("unexpected IPs: %v", ips)
	}
}

func TestExtractIPsFromBody_Regex(t *testing.T) {
	body := []byte("10.0.0.1\n10.0.0.2\n")
	ips := extractIPsFromBody(body)
	if len(ips) != 2 || ips[0] != "10.0.0.1" || ips[1] != "10.0.0.2" {
		t.Errorf("unexpected IPs: %v", ips)
	}
}

func TestExtractIPsFromBody_Empty(t *testing.T) {
	body := []byte(`{"Answer":[]}`)
	ips := extractIPsFromBody(body)
	if len(ips) != 0 {
		t.Errorf("expected 0 IPs, got %v", ips)
	}
}

func TestMergeIPs(t *testing.T) {
	cf := []string{"1.1.1.1", "2.2.2.2"}
	ali := []string{"2.2.2.2", "3.3.3.3"}
	merged := mergeIPs(cf, ali)
	want := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	if len(merged) != len(want) {
		t.Fatalf("mergeIPs length = %d, want %d", len(merged), len(want))
	}
	for i := range want {
		if merged[i] != want[i] {
			t.Errorf("mergeIPs[%d] = %q, want %q", i, merged[i], want[i])
		}
	}
}

func TestMergeIPs_CFFirst(t *testing.T) {
	cf := []string{"10.0.0.1"}
	ali := []string{"10.0.0.2"}
	merged := mergeIPs(cf, ali)
	if len(merged) != 2 || merged[0] != "10.0.0.1" || merged[1] != "10.0.0.2" {
		t.Errorf("expected CF first, got %v", merged)
	}
}

func useDoHTestConfig(t *testing.T, client *http.Client, timeout time.Duration, cfTemplate, aliTemplate string) {
	oldCFTemplate := cfDoHURLTemplate
	oldAliTemplate := aliDoHURLTemplate
	oldHTTPClient := dohHTTPClient
	oldTimeout := dohTimeout
	t.Cleanup(func() {
		cfDoHURLTemplate = oldCFTemplate
		aliDoHURLTemplate = oldAliTemplate
		dohHTTPClient = oldHTTPClient
		dohTimeout = oldTimeout
	})

	cfDoHURLTemplate = cfTemplate
	aliDoHURLTemplate = aliTemplate
	dohHTTPClient = client
	dohTimeout = timeout
}

func TestResolveDoHDual_BothSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cf":
			if got := r.Header.Get("Accept"); got != "application/dns-json" {
				t.Errorf("CF request missing Accept header, got %q", got)
			}
			fmt.Fprint(w, `{"Answer":[{"data":"1.1.1.1"},{"data":"2.2.2.2"}]}`)
		case "/ali":
			fmt.Fprint(w, `{"Answer":[{"data":"2.2.2.2"},{"data":"3.3.3.3"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useDoHTestConfig(
		t,
		srv.Client(),
		time.Second,
		srv.URL+"/cf?name=%s&type=A",
		srv.URL+"/ali?name=%s&type=A&short=1",
	)

	ips, cfTimedOut, aliTimedOut := resolveDoHDual(context.Background(), "example.com")
	want := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	if !reflect.DeepEqual(ips, want) {
		t.Fatalf("resolveDoHDual IPs = %v, want %v", ips, want)
	}
	if cfTimedOut || aliTimedOut {
		t.Fatalf("unexpected timeout flags: cf=%v ali=%v", cfTimedOut, aliTimedOut)
	}
}

func TestResolveDoHDual_CFTimeoutAliSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cf":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			fmt.Fprint(w, `{"Answer":[{"data":"1.1.1.1"}]}`)
		case "/ali":
			fmt.Fprint(w, `{"Answer":[{"data":"3.3.3.3"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useDoHTestConfig(
		t,
		srv.Client(),
		50*time.Millisecond,
		srv.URL+"/cf?name=%s&type=A",
		srv.URL+"/ali?name=%s&type=A&short=1",
	)

	ips, cfTimedOut, aliTimedOut := resolveDoHDual(context.Background(), "example.com")
	want := []string{"3.3.3.3"}
	if !reflect.DeepEqual(ips, want) {
		t.Fatalf("resolveDoHDual IPs = %v, want %v", ips, want)
	}
	if !cfTimedOut || aliTimedOut {
		t.Fatalf("unexpected timeout flags: cf=%v ali=%v", cfTimedOut, aliTimedOut)
	}
}

func TestResolveDoHDual_AliTimeoutCFSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cf":
			fmt.Fprint(w, `{"Answer":[{"data":"1.1.1.1"}]}`)
		case "/ali":
			select {
			case <-r.Context().Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
			fmt.Fprint(w, `{"Answer":[{"data":"3.3.3.3"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useDoHTestConfig(
		t,
		srv.Client(),
		50*time.Millisecond,
		srv.URL+"/cf?name=%s&type=A",
		srv.URL+"/ali?name=%s&type=A&short=1",
	)

	ips, cfTimedOut, aliTimedOut := resolveDoHDual(context.Background(), "example.com")
	want := []string{"1.1.1.1"}
	if !reflect.DeepEqual(ips, want) {
		t.Fatalf("resolveDoHDual IPs = %v, want %v", ips, want)
	}
	if cfTimedOut || !aliTimedOut {
		t.Fatalf("unexpected timeout flags: cf=%v ali=%v", cfTimedOut, aliTimedOut)
	}
}

func TestResolveDoHDual_BothTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
		fmt.Fprint(w, `{"Answer":[{"data":"1.1.1.1"}]}`)
	}))
	defer srv.Close()
	useDoHTestConfig(
		t,
		srv.Client(),
		50*time.Millisecond,
		srv.URL+"/cf?name=%s&type=A",
		srv.URL+"/ali?name=%s&type=A&short=1",
	)

	ips, cfTimedOut, aliTimedOut := resolveDoHDual(context.Background(), "example.com")
	if len(ips) != 0 {
		t.Fatalf("expected no IPs when both providers timeout, got %v", ips)
	}
	if !cfTimedOut || !aliTimedOut {
		t.Fatalf("expected both providers timeout, cf=%v ali=%v", cfTimedOut, aliTimedOut)
	}
}

func TestResolveDoHDual_BothNoIPsWithoutTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Answer":[]}`)
	}))
	defer srv.Close()
	useDoHTestConfig(
		t,
		srv.Client(),
		time.Second,
		srv.URL+"/cf?name=%s&type=A",
		srv.URL+"/ali?name=%s&type=A&short=1",
	)

	ips, cfTimedOut, aliTimedOut := resolveDoHDual(context.Background(), "example.com")
	if len(ips) != 0 {
		t.Fatalf("expected no IPs, got %v", ips)
	}
	if cfTimedOut || aliTimedOut {
		t.Fatalf("did not expect timeout flags, cf=%v ali=%v", cfTimedOut, aliTimedOut)
	}
}

func TestIsTimeoutErr(t *testing.T) {
	if isTimeoutErr(nil) {
		t.Error("nil should not be timeout")
	}
	if !isTimeoutErr(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded should be timeout")
	}
	if isTimeoutErr(fmt.Errorf("random error")) {
		t.Error("random error should not be timeout")
	}
}
