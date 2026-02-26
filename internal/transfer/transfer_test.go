package transfer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

func TestZeroReader(t *testing.T) {
	r := &zeroReader{remaining: 100}
	buf := make([]byte, 50)
	n, err := r.Read(buf)
	if n != 50 || err != nil {
		t.Errorf("Read(50) = %d, %v", n, err)
	}
	n, err = r.Read(buf)
	if n != 50 || err != nil {
		t.Errorf("Read(50) = %d, %v", n, err)
	}
	n, err = r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Errorf("Read(0) = %d, %v", n, err)
	}
}

func TestCountingReader(t *testing.T) {
	cr := &countingReader{r: &zeroReader{remaining: 200}}
	buf := make([]byte, 80)
	cr.Read(buf)
	cr.Read(buf)
	cr.Read(buf) // reads 40 remaining
	if cr.count.Load() != 200 {
		t.Errorf("count = %d, want 200", cr.count.Load())
	}
}

func newTestBus() *render.Bus {
	return render.NewBus(render.NewPlainRenderer(&strings.Builder{}))
}

func TestDownloadIntegration(t *testing.T) {
	// Create a server that returns 1MB of data
	data := make([]byte, 1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 2 * 1024 * 1024,
		Timeout:  5,
		Max:      "2M",
	}
	bus := newTestBus()
	defer bus.Close()
	client := srv.Client()

	res := Run(context.Background(), client, cfg, Download, 1, srv.URL, bus)
	if res.TotalBytes == 0 {
		t.Error("downloaded 0 bytes")
	}
	if res.Mbps <= 0 {
		t.Error("Mbps <= 0")
	}
	if res.Direction != Download {
		t.Errorf("Direction = %v", res.Direction)
	}
}

func TestUploadIntegration(t *testing.T) {
	var received int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		received = n
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 512 * 1024,
		Timeout:  5,
		Max:      "512K",
	}
	bus := newTestBus()
	defer bus.Close()
	client := srv.Client()

	res := Run(context.Background(), client, cfg, Upload, 1, srv.URL, bus)
	if res.TotalBytes == 0 {
		t.Error("uploaded 0 bytes")
	}
	if received == 0 {
		t.Error("server received 0 bytes")
	}
}

func TestMultiThreadDownload(t *testing.T) {
	data := make([]byte, 512*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 1024 * 1024,
		Timeout:  5,
		Max:      "1M",
	}
	bus := newTestBus()
	defer bus.Close()
	client := srv.Client()

	res := Run(context.Background(), client, cfg, Download, 4, srv.URL, bus)
	if res.TotalBytes == 0 {
		t.Error("downloaded 0 bytes with 4 threads")
	}
	if res.Threads != 4 {
		t.Errorf("Threads = %d", res.Threads)
	}
}

func TestDownloadTimeout(t *testing.T) {
	// Server that sends data very slowly, but respects client disconnect.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			w.Write([]byte("x"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		MaxBytes: 1024 * 1024 * 1024,
		Timeout:  1,
		Max:      "1G",
	}
	bus := newTestBus()
	defer bus.Close()
	client := srv.Client()

	start := time.Now()
	Run(context.Background(), client, cfg, Download, 1, srv.URL, bus)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("timeout did not work, took %v", elapsed)
	}
}

func TestDirectionString(t *testing.T) {
	if Download.String() != "Download" {
		t.Error("Download.String()")
	}
	if Upload.String() != "Upload" {
		t.Error("Upload.String()")
	}
}
