package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/endpoint"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
	"testing"
)

func TestFormatLocation(t *testing.T) {
	tests := []struct {
		name string
		info endpoint.IPInfo
		want string
	}{
		{"empty", endpoint.IPInfo{}, "unavailable"},
		{"city_only", endpoint.IPInfo{City: "Tokyo"}, "Tokyo"},
		{"full", endpoint.IPInfo{City: "Tokyo", RegionName: "Kanto", Country: "Japan"}, "Tokyo, Kanto, Japan"},
		{"city_eq_region", endpoint.IPInfo{City: "Tokyo", RegionName: "Tokyo", Country: "Japan"}, "Tokyo, Japan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLocation(tt.info)
			if got != tt.want {
				t.Errorf("formatLocation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunSkipsDuplicateRoundsWhenThreadsIsOne(t *testing.T) {
	srv := mockRunnerServer()
	defer srv.Close()

	host := endpoint.HostFromURL(srv.URL)
	cfg := &config.Config{
		DLURL:          srv.URL + "/large",
		ULURL:          srv.URL + "/slurp",
		LatencyURL:     srv.URL + "/small",
		Max:            "512K",
		MaxBytes:       512 * 1024,
		Timeout:        2,
		Threads:        1,
		LatencyCount:   2,
		EndpointIP:     host,
		NoMetadata:     true,
		NonInteractive: true,
	}

	bus := render.NewBus(render.NewPlainRenderer(&strings.Builder{}))
	defer bus.Close()

	result := Run(context.Background(), cfg, bus, false)
	if len(result.Rounds) != 2 {
		t.Fatalf("expected 2 rounds, got %d", len(result.Rounds))
	}
	for _, round := range result.Rounds {
		if round.Threads != 1 {
			t.Fatalf("expected single-thread round, got %+v", round)
		}
	}
}

func TestRunWarnsOnMixedHosts(t *testing.T) {
	srv := mockRunnerServer()
	defer srv.Close()

	altURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	cfg := &config.Config{
		DLURL:          srv.URL + "/large",
		ULURL:          altURL + "/slurp",
		LatencyURL:     srv.URL + "/small",
		Max:            "256K",
		MaxBytes:       256 * 1024,
		Timeout:        2,
		Threads:        1,
		LatencyCount:   1,
		NoMetadata:     true,
		NonInteractive: true,
	}

	bus := render.NewBus(render.NewPlainRenderer(&strings.Builder{}))
	defer bus.Close()

	result := Run(context.Background(), cfg, bus, false)
	if !result.Degraded {
		t.Fatal("expected degraded result for mixed hosts")
	}
	found := false
	for _, warning := range result.Warnings {
		if warning.Code == "mixed_hosts" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mixed_hosts warning, got %+v", result.Warnings)
	}
	if result.ConnectionInfo.MetadataEnabled {
		t.Fatal("expected metadata to be disabled in result")
	}
}

func TestRunResultJSONGolden(t *testing.T) {
	fixture := RunResult{
		SchemaVersion: 1,
		Config: RunConfig{
			DLURL:          "https://example.com/dl",
			ULURL:          "https://example.com/ul",
			LatencyURL:     "https://example.com/ping",
			Max:            "1G",
			MaxBytes:       1000000000,
			TimeoutSeconds: 10,
			Threads:        4,
			LatencyCount:   20,
			JSON:           true,
			NonInteractive: true,
			EndpointIP:     "1.1.1.1",
			Metadata:       false,
		},
		Candidates: []CandidateResult{{
			IP:          "1.1.1.1",
			Description: "Tokyo, Japan",
			RTTMs:       floatPtr(12.34),
			Source:      "user",
			Status:      "ok",
		}},
		SelectedEndpoint: SelectedEndpoint{
			IP:          "1.1.1.1",
			Description: "Tokyo, Japan",
			RTTMs:       floatPtr(12.34),
			Source:      "user",
			Status:      "ok",
		},
		ConnectionInfo: ConnectionInfo{
			Status:          "unavailable",
			MetadataEnabled: false,
			Host:            "example.com",
			Client:          PeerInfo{Status: "unavailable"},
			Server:          PeerInfo{Status: "unavailable"},
		},
		IdleLatency: LatencyResult{
			Status:   "ok",
			Samples:  2,
			MinMs:    floatPtr(10.1),
			AvgMs:    floatPtr(11.2),
			MedianMs: floatPtr(11.1),
			MaxMs:    floatPtr(12.3),
			JitterMs: floatPtr(1.1),
		},
		Rounds: []RoundResult{{
			Name:       "Download (single thread)",
			Direction:  "download",
			Threads:    1,
			Status:     "ok",
			URL:        "https://example.com/dl",
			TotalBytes: 123456,
			DurationMs: 500,
			Mbps:       19.75,
			LoadedLatency: LatencyResult{
				Status:   "ok",
				Samples:  1,
				MinMs:    floatPtr(20),
				AvgMs:    floatPtr(20),
				MedianMs: floatPtr(20),
				MaxMs:    floatPtr(20),
				JitterMs: floatPtr(0),
			},
		}},
		TotalBytes: 123456,
		Warnings: []Warning{{
			Code:    "mixed_hosts",
			Message: "DL_URL, UL_URL and LATENCY_URL hosts differ. Shared endpoint pinning is disabled.",
		}},
		Degraded:   true,
		ExitCode:   2,
		StartedAt:  "2026-03-15T00:00:00Z",
		DurationMs: 500,
	}

	got, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error: %v", err)
	}
	want, err := os.ReadFile("testdata/run_result.golden.json")
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	gotText := normalizeGolden(got)
	wantText := normalizeGolden(want)
	if gotText != wantText {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", gotText, wantText)
	}
}

func normalizeGolden(data []byte) string {
	return strings.ReplaceAll(strings.TrimSpace(string(data)), "\r\n", "\n")
}

func mockRunnerServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/small", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 256*1024))
	})
	mux.HandleFunc("/slurp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("ok"))
	})
	return httptest.NewServer(mux)
}
