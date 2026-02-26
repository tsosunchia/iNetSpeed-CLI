package speedtest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/latency"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/transfer"
)

// mockCDN creates a test server that mimics the Apple CDN endpoints.
func mockCDN() *httptest.Server {
	mux := http.NewServeMux()

	// /small – latency endpoint (returns tiny body)
	mux.HandleFunc("/small", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// /large – download endpoint (returns configurable size)
	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		data := make([]byte, 256*1024) // 256 KiB chunks
		for i := 0; i < 8; i++ {       // 2 MiB total
			w.Write(data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	})

	// /slurp – upload endpoint
	mux.HandleFunc("/slurp", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	})

	return httptest.NewServer(mux)
}

func TestIntegrationDownloadSingleThread(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 4 * 1024 * 1024,
		Timeout:  5,
		Max:      "4M",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	res := transfer.Run(context.Background(), srv.Client(), cfg,
		transfer.Download, 1, srv.URL+"/large", bus)

	if res.TotalBytes == 0 {
		t.Error("downloaded 0 bytes")
	}
	if res.Mbps <= 0 {
		t.Error("Mbps <= 0")
	}
}

func TestIntegrationUploadSingleThread(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 512 * 1024,
		Timeout:  5,
		Max:      "512K",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	res := transfer.Run(context.Background(), srv.Client(), cfg,
		transfer.Upload, 1, srv.URL+"/slurp", bus)

	if res.TotalBytes == 0 {
		t.Error("uploaded 0 bytes")
	}
}

func TestIntegrationMultiThreadDownload(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 4 * 1024 * 1024,
		Timeout:  5,
		Max:      "4M",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	res := transfer.Run(context.Background(), srv.Client(), cfg,
		transfer.Download, 4, srv.URL+"/large", bus)

	if res.TotalBytes == 0 {
		t.Error("downloaded 0 bytes with 4 threads")
	}
}

func TestIntegrationIdleLatency(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	stats := latency.MeasureIdle(context.Background(), srv.Client(), srv.URL+"/small", 5)
	if stats.N != 5 {
		t.Errorf("N = %d, want 5", stats.N)
	}
	if stats.Min <= 0 {
		t.Error("Min <= 0")
	}
	if stats.Median <= 0 {
		t.Error("Median <= 0")
	}
}

func TestIntegrationLoadedLatency(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	probe := latency.StartLoaded(context.Background(), srv.Client(), srv.URL+"/small")
	time.Sleep(500 * time.Millisecond)
	stats := probe.Stop()
	if stats.N == 0 {
		t.Error("no loaded latency samples collected")
	}
}

// Test that DoH returns expected structure
func TestDoHResponseParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Answer":[{"data":"1.2.3.4"},{"data":"5.6.7.8"}]}`)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Answer) != 2 {
		t.Errorf("expected 2 answers, got %d", len(result.Answer))
	}
}

func TestDoHEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Answer) != 0 {
		t.Errorf("expected 0 answers for empty response")
	}
}

func TestDoHErrorJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	resp2, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var result struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	err = json.NewDecoder(resp2.Body).Decode(&result)
	if err == nil {
		t.Error("expected JSON parse error")
	}
}

func TestIPApiMockSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"query":   "1.2.3.4",
			"as":      "AS1234",
			"isp":     "TestISP",
			"city":    "Tokyo",
			"country": "Japan",
		})
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info struct {
		Status  string `json:"status"`
		City    string `json:"city"`
		Country string `json:"country"`
	}
	json.NewDecoder(resp.Body).Decode(&info)
	if info.Status != "success" || info.City != "Tokyo" {
		t.Errorf("unexpected: %+v", info)
	}
}

func TestIPApiTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// Streaming progress test – uses a slow server so the 500ms ticker fires.
func TestStreamingProgress(t *testing.T) {
	// Slow server: drips 64KiB chunks every 200ms for ~2s.
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := make([]byte, 64*1024)
		for i := 0; i < 10; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(200 * time.Millisecond)
		}
	}))
	defer slowSrv.Close()

	cfg := &config.Config{
		MaxBytes: 10 * 1024 * 1024,
		Timeout:  5,
		Max:      "10M",
	}

	var events []render.Event
	collector := &eventCollector{events: &events}
	bus := render.NewBus(collector)

	transfer.Run(context.Background(), slowSrv.Client(), cfg,
		transfer.Download, 1, slowSrv.URL, bus)
	bus.Close()

	// Check that progress events were emitted during execution, not just at the end
	hasProgress := false
	for _, ev := range events {
		if ev.Kind == render.KindProgress {
			hasProgress = true
			break
		}
	}
	if !hasProgress {
		t.Error("no progress events emitted during transfer")
	}
}

type eventCollector struct {
	events *[]render.Event
}

func (c *eventCollector) Render(ev render.Event) {
	*c.events = append(*c.events, ev)
}

// Smoke test: ensure all phases produce output
func TestSmokeAllPhases(t *testing.T) {
	srv := mockCDN()
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 1024 * 1024,
		Timeout:  2,
		Max:      "1M",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))

	// Run all four transfer tests
	for _, dir := range []transfer.Direction{transfer.Download, transfer.Upload} {
		for _, threads := range []int{1, 2} {
			transfer.Run(context.Background(), srv.Client(), cfg, dir, threads, srv.URL+"/large", bus)
		}
	}

	// Idle latency
	stats := latency.MeasureIdle(context.Background(), srv.Client(), srv.URL+"/small", 3)
	bus.Info(fmt.Sprintf("Idle: %.2f ms", stats.Median))

	bus.Close()

	out := buf.String()
	if !strings.Contains(out, "Idle:") {
		t.Error("missing idle latency in output")
	}
}

// Test that context cancellation stops transfer promptly
func TestContextCancellation(t *testing.T) {
	// Slow server that drips data forever
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			w.Write(make([]byte, 1024))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 1024 * 1024 * 1024, // 1 GiB — way more than we'll transfer
		Timeout:  30,
		Max:      "1G",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 500ms
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	transfer.Run(ctx, srv.Client(), cfg, transfer.Download, 1, srv.URL, bus)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("transfer should have stopped after cancellation, took %v", elapsed)
	}
}

// Test upload to server returning 4xx
func TestUploadBadStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Forbidden"))
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 256 * 1024,
		Timeout:  2,
		Max:      "256K",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	res := transfer.Run(context.Background(), srv.Client(), cfg,
		transfer.Upload, 1, srv.URL, bus)

	if res.TotalBytes != 0 {
		t.Errorf("expected 0 bytes from 403 upload, got %d", res.TotalBytes)
	}
}

// Test download from server returning 4xx
func TestDownloadBadStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Forbidden"))
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 1024 * 1024,
		Timeout:  2,
		Max:      "1M",
	}

	var buf bytes.Buffer
	bus := render.NewBus(render.NewPlainRenderer(&buf))
	defer bus.Close()

	res := transfer.Run(context.Background(), srv.Client(), cfg,
		transfer.Download, 1, srv.URL, bus)

	if res.TotalBytes != 0 {
		t.Errorf("expected 0 bytes from 403 server, got %d", res.TotalBytes)
	}
}
