package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/endpoint"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/latency"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/netx"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/transfer"
)

// Run executes the full speedtest pipeline. Exit codes: 0 success, 2 degraded, 130 interrupted.
func Run(ctx context.Context, cfg *config.Config, bus *render.Bus, isTTY bool) int {
	degraded := false

	bus.Line()
	bus.Banner("\u26a1 Apple CDN Speedtest")
	bus.Info("Config:  " + cfg.Summary())
	bus.Line()

	bus.Header("Environment Check")
	bus.Info("Go binary \u2014 no external dependencies required.")

	if ctx.Err() != nil {
		bus.Warn("Interrupted.")
		return 130
	}

	cdnHost := endpoint.HostFromURL(cfg.DLURL)
	ep := endpoint.Choose(ctx, cdnHost, bus, isTTY)

	clientOpts := netx.Options{
		Timeout: time.Duration(cfg.Timeout+5) * time.Second,
	}
	if ep.IP != "" && cdnHost != "" {
		clientOpts.PinHost = cdnHost
		clientOpts.PinIP = ep.IP
	}
	client := netx.NewClient(clientOpts)

	if ctx.Err() != nil {
		bus.Warn("Interrupted.")
		return 130
	}

	if !gatherInfo(ctx, bus, cdnHost, ep) {
		degraded = true
	}

	if ctx.Err() != nil {
		bus.Warn("Interrupted.")
		return 130
	}

	bus.Header("Idle Latency")
	bus.Info("Endpoint: " + cfg.LatencyURL)
	bus.Info(fmt.Sprintf("Samples: %d", cfg.LatencyCount))

	idleStats := latency.MeasureIdle(ctx, client, cfg.LatencyURL, cfg.LatencyCount)
	bus.Result(fmt.Sprintf("%.2f ms median  (min %.2f / avg %.2f / max %.2f)  jitter %.2f ms",
		idleStats.Median, idleStats.Min, idleStats.Avg, idleStats.Max, idleStats.Jitter))

	var totalData int64

	runRound := func(dir transfer.Direction, threads int, label string, url string) {
		if ctx.Err() != nil {
			return
		}
		bus.Header(label)
		bus.Info(fmt.Sprintf("Threads: %d", threads))
		bus.Info(fmt.Sprintf("Limit: %s / %ds per thread", cfg.Max, cfg.Timeout))

		loadedProbe := latency.StartLoaded(ctx, client, cfg.LatencyURL)
		res := transfer.Run(ctx, client, cfg, dir, threads, url, bus)
		loadedStats := loadedProbe.Stop()
		totalData += res.TotalBytes

		if threads <= 1 {
			bus.Result(fmt.Sprintf("%.0f Mbps  (%s in %.1fs)",
				res.Mbps, config.HumanBytes(res.TotalBytes), res.Duration.Seconds()))
		} else {
			bus.Result(fmt.Sprintf("%.0f Mbps  (%s in %.1fs, %d threads)",
				res.Mbps, config.HumanBytes(res.TotalBytes), res.Duration.Seconds(), threads))
		}
		bus.Info(fmt.Sprintf("Loaded latency: %.2f ms  (jitter %.2f ms)",
			loadedStats.Median, loadedStats.Jitter))
	}

	runRound(transfer.Download, 1, "Download (single thread)", cfg.DLURL)
	runRound(transfer.Download, cfg.Threads, "Download (multi-thread)", cfg.DLURL)
	runRound(transfer.Upload, 1, "Upload (single thread)", cfg.ULURL)
	runRound(transfer.Upload, cfg.Threads, "Upload (multi-thread)", cfg.ULURL)

	if ctx.Err() != nil {
		bus.Warn("Interrupted.")
		return 130
	}

	bus.Line()
	bus.Banner("\U0001f4ca Summary")
	bus.Line()
	bus.KV("Idle Latency", fmt.Sprintf("%.2f ms  (jitter %.2f ms)", idleStats.Median, idleStats.Jitter))
	bus.KV("Data Used", config.HumanBytes(totalData))
	bus.Line()
	bus.Info("All tests complete.")
	bus.Line()

	if degraded {
		return 2
	}
	return 0
}

func gatherInfo(ctx context.Context, bus *render.Bus, host string, ep endpoint.Endpoint) bool {
	ok := true
	bus.Header("Connection Information")

	cinfo := endpoint.FetchInfo(ctx, "")
	clientIP := cinfo.Query
	if clientIP == "" {
		clientIP = "?"
		ok = false
	}
	clientISP := cinfo.ISP
	if clientISP == "" {
		clientISP = "?"
	}
	clientAS := cinfo.AS
	if clientAS == "" {
		clientAS = "?"
	}
	clientLoc := formatLocation(cinfo)

	bus.KV("Client", fmt.Sprintf("%s  (%s)", clientIP, clientISP))
	bus.KV("  ASN", clientAS)
	bus.KV("  Location", clientLoc)

	serverIP := ep.IP
	if serverIP == "" && host != "" {
		// DNS fallback: resolve host to enrich server metadata
		serverIP = endpoint.ResolveHost(host)
	}
	if serverIP == "" {
		serverIP = "?"
		ok = false
	}
	bus.KV("Server", fmt.Sprintf("%s  \u2192  %s", host, serverIP))
	if ep.Desc != "" {
		bus.KV("  Endpoint", ep.Desc)
	}

	if serverIP != "?" {
		sinfo := endpoint.FetchInfo(ctx, serverIP)
		sAS := sinfo.AS
		if sAS == "" {
			sAS = sinfo.Org
		}
		if sAS == "" {
			sAS = "?"
		}
		sLoc := formatLocation(sinfo)
		bus.KV("  ASN", sAS)
		bus.KV("  Location", sLoc)
	}

	return ok
}

func formatLocation(info endpoint.IPInfo) string {
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
		loc = "?"
	}
	return loc
}
