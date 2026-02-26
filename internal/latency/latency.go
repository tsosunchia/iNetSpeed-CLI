package latency

import (
	"context"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
)

type Stats struct {
	Min    float64
	Avg    float64
	Median float64
	Max    float64
	Jitter float64
	N      int
}

func MeasureIdle(ctx context.Context, client *http.Client, url string, n int) Stats {
	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			break
		}
		d := probe(ctx, client, url)
		if d >= 0 {
			samples = append(samples, d)
		}
	}
	return Compute(samples)
}

type Probe struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	client  *http.Client
	url     string
	samples []float64
	wg      sync.WaitGroup
}

func StartLoaded(ctx context.Context, client *http.Client, url string) *Probe {
	ctx2, cancel := context.WithCancel(ctx)
	p := &Probe{
		ctx:    ctx2,
		cancel: cancel,
		client: client,
		url:    url,
	}
	p.wg.Add(1)
	go p.loop()
	return p
}

func (p *Probe) loop() {
	defer p.wg.Done()
	for {
		if p.ctx.Err() != nil {
			return
		}
		d := probe(p.ctx, p.client, p.url)
		if d >= 0 {
			p.mu.Lock()
			p.samples = append(p.samples, d)
			p.mu.Unlock()
		}
	}
}

func (p *Probe) Stop() Stats {
	p.cancel()
	p.wg.Wait()
	p.mu.Lock()
	s := p.samples
	p.mu.Unlock()
	return Compute(s)
}

func probe(ctx context.Context, client *http.Client, url string) float64 {
	ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx2, http.MethodGet, url, nil)
	if err != nil {
		return -1
	}
	req.Header.Set("User-Agent", config.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh-Hans;q=0.9")
	req.Header.Set("Accept-Encoding", "identity")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	for {
		_, e := resp.Body.Read(buf)
		if e != nil {
			break
		}
	}
	return float64(time.Since(start).Microseconds()) / 1000.0
}

func Compute(samples []float64) Stats {
	n := len(samples)
	if n == 0 {
		return Stats{}
	}
	sorted := make([]float64, n)
	copy(sorted, samples)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	avg := sum / float64(n)
	min := sorted[0]
	max := sorted[n-1]

	var med float64
	if n%2 == 1 {
		med = sorted[n/2]
	} else {
		med = (sorted[n/2-1] + sorted[n/2]) / 2
	}

	var jitter float64
	if n > 1 {
		for i := 1; i < n; i++ {
			jitter += math.Abs(sorted[i] - sorted[i-1])
		}
		jitter /= float64(n - 1)
	}

	return Stats{
		Min:    math.Round(min*100) / 100,
		Avg:    math.Round(avg*100) / 100,
		Median: math.Round(med*100) / 100,
		Max:    math.Round(max*100) / 100,
		Jitter: math.Round(jitter*100) / 100,
		N:      n,
	}
}
