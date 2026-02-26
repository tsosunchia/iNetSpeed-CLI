package transfer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
)

type Direction int

const (
	Download Direction = iota
	Upload
)

func (d Direction) String() string {
	if d == Download {
		return "Download"
	}
	return "Upload"
}

type Result struct {
	Direction  Direction
	Threads    int
	TotalBytes int64
	Duration   time.Duration
	Mbps       float64
}

func Run(ctx context.Context, client *http.Client, cfg *config.Config,
	dir Direction, threads int, url string, bus *render.Bus) Result {

	maxBytes := cfg.MaxBytes
	timeout := time.Duration(cfg.Timeout) * time.Second

	var totalBytes int64
	var wg sync.WaitGroup

	ctx2, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()

	start := time.Now()

	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cur := atomic.LoadInt64(&totalBytes)
				elapsed := time.Since(start).Seconds()
				if elapsed > 0 {
					mbps := float64(cur) * 8 / (elapsed * 1_000_000)
					bus.Progress(dir.String(),
						fmt.Sprintf("%.1f Mbps  %s  %.1fs",
							mbps, config.HumanBytes(cur), elapsed))
				}
			case <-ctx2.Done():
				return
			}
		}
	}()

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if dir == Download {
				doDownload(ctx2, client, url, maxBytes, timeout, &totalBytes)
			} else {
				doUpload(ctx2, client, url, maxBytes, timeout, &totalBytes)
			}
		}()
	}

	wg.Wait()
	cancel()
	<-progressDone

	dur := time.Since(start)
	total := atomic.LoadInt64(&totalBytes)
	secs := dur.Seconds()
	if secs <= 0 {
		secs = 1
	}
	mbps := float64(total) * 8 / (secs * 1_000_000)

	return Result{
		Direction:  dir,
		Threads:    threads,
		TotalBytes: total,
		Duration:   dur,
		Mbps:       mbps,
	}
}

func doDownload(ctx context.Context, client *http.Client, url string, maxBytes int64, timeout time.Duration, shared *int64) int64 {
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", config.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh-Hans;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0
	}

	buf := make([]byte, 256*1024)
	var total int64
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			atomic.AddInt64(shared, int64(n))
		}
		if total >= maxBytes {
			break
		}
		if e != nil {
			break
		}
	}
	return total
}

type zeroReader struct {
	remaining int64
}

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > z.remaining {
		n = z.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 0
	}
	z.remaining -= n
	return int(n), nil
}

type countingReader struct {
	r      io.Reader
	count  atomic.Int64
	shared *int64 // shared counter updated atomically during transfer
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.count.Add(int64(n))
		if c.shared != nil {
			atomic.AddInt64(c.shared, int64(n))
		}
	}
	return n, err
}

func doUpload(ctx context.Context, client *http.Client, url string, maxBytes int64, timeout time.Duration, shared *int64) int64 {
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cr := &countingReader{
		r:      &zeroReader{remaining: maxBytes},
		shared: shared,
	}

	req, err := http.NewRequestWithContext(ctx2, http.MethodPut, url, cr)
	if err != nil {
		return 0
	}
	req.ContentLength = -1
	req.Header.Set("User-Agent", config.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh-Hans;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Upload-Draft-Interop-Version", "6")
	req.Header.Set("Upload-Complete", "?1")

	resp, err := client.Do(req)
	if err != nil {
		return cr.count.Load()
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		sent := cr.count.Load()
		atomic.AddInt64(shared, -sent) // rollback shared counter
		return 0
	}
	return cr.count.Load()
}
