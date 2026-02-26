package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/endpoint"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/latency"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/netx"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/transfer"
)

// Run executes the full speedtest pipeline. Exit codes: 0 success, 2 degraded, 130 interrupted.
func Run(ctx context.Context, cfg *config.Config, bus *render.Bus, isTTY bool) int {
	degraded := false

	bus.Line()
	bus.Banner("\u26a1 iNetSpeed-CLI")
	bus.Info(i18n.Text("Config:  ", "配置:  ") + cfg.Summary())
	bus.Line()

	bus.Header(i18n.Text("Environment Check", "环境检查"))
	bus.Info(i18n.Text("Go binary \u2014 no external dependencies required.", "Go 二进制程序 — 无需外部依赖。"))

	if ctx.Err() != nil {
		bus.Warn(i18n.Text("Interrupted.", "已中断。"))
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
		bus.Warn(i18n.Text("Interrupted.", "已中断。"))
		return 130
	}

	if !gatherInfo(ctx, bus, cdnHost, ep) {
		degraded = true
	}

	if ctx.Err() != nil {
		bus.Warn(i18n.Text("Interrupted.", "已中断。"))
		return 130
	}

	bus.Header(i18n.Text("Idle Latency", "空载延迟"))
	bus.Info(i18n.Text("Endpoint: ", "端点: ") + cfg.LatencyURL)
	bus.Info(fmt.Sprintf(i18n.Text("Samples: %d", "采样: %d"), cfg.LatencyCount))

	idleStats := latency.MeasureIdle(ctx, client, cfg.LatencyURL, cfg.LatencyCount)
	bus.Result(fmt.Sprintf(i18n.Text(
		"%.2f ms median  (min %.2f / avg %.2f / max %.2f)  jitter %.2f ms",
		"%.2f 毫秒 中位数  (最小 %.2f / 平均 %.2f / 最大 %.2f)  抖动 %.2f 毫秒"),
		idleStats.Median, idleStats.Min, idleStats.Avg, idleStats.Max, idleStats.Jitter))

	var totalData int64

	runRound := func(dir transfer.Direction, threads int, label string, url string) {
		if ctx.Err() != nil {
			return
		}
		bus.Header(label)
		bus.Info(fmt.Sprintf(i18n.Text("Threads: %d", "线程: %d"), threads))
		bus.Info(fmt.Sprintf(i18n.Text("Limit: %s / %ds per thread", "上限: %s / 每线程 %ds"), cfg.Max, cfg.Timeout))

		loadedProbe := latency.StartLoaded(ctx, client, cfg.LatencyURL)
		res := transfer.Run(ctx, client, cfg, dir, threads, url, bus)
		loadedStats := loadedProbe.Stop()
		totalData += res.TotalBytes

		if threads <= 1 {
			bus.Result(fmt.Sprintf(i18n.Text("%.0f Mbps  (%s in %.1fs)", "%.0f Mbps  (%s，耗时 %.1fs)"),
				res.Mbps, config.HumanBytes(res.TotalBytes), res.Duration.Seconds()))
		} else {
			bus.Result(fmt.Sprintf(i18n.Text("%.0f Mbps  (%s in %.1fs, %d threads)", "%.0f Mbps  (%s，耗时 %.1fs，%d 线程)"),
				res.Mbps, config.HumanBytes(res.TotalBytes), res.Duration.Seconds(), threads))
		}
		bus.Info(fmt.Sprintf(i18n.Text("Loaded latency: %.2f ms  (jitter %.2f ms)", "负载延迟: %.2f 毫秒  (抖动 %.2f 毫秒)"),
			loadedStats.Median, loadedStats.Jitter))
	}

	runRound(transfer.Download, 1, i18n.Text("Download (single thread)", "下载（单线程）"), cfg.DLURL)
	runRound(transfer.Download, cfg.Threads, i18n.Text("Download (multi-thread)", "下载（多线程）"), cfg.DLURL)
	runRound(transfer.Upload, 1, i18n.Text("Upload (single thread)", "上传（单线程）"), cfg.ULURL)
	runRound(transfer.Upload, cfg.Threads, i18n.Text("Upload (multi-thread)", "上传（多线程）"), cfg.ULURL)

	if ctx.Err() != nil {
		bus.Warn(i18n.Text("Interrupted.", "已中断。"))
		return 130
	}

	bus.Line()
	bus.Banner(i18n.Text("\U0001f4ca Summary", "\U0001f4ca 测速汇总"))
	bus.Line()
	bus.KV(i18n.Text("Idle Latency", "空载延迟"), fmt.Sprintf(i18n.Text("%.2f ms  (jitter %.2f ms)", "%.2f 毫秒  (抖动 %.2f 毫秒)"), idleStats.Median, idleStats.Jitter))
	bus.KV(i18n.Text("Data Used", "消耗流量"), config.HumanBytes(totalData))
	bus.Line()
	bus.Info(i18n.Text("All tests complete.", "所有测试完成。"))
	bus.Line()

	if degraded {
		return 2
	}
	return 0
}

func gatherInfo(ctx context.Context, bus *render.Bus, host string, ep endpoint.Endpoint) bool {
	ok := true
	bus.Header(i18n.Text("Connection Information", "连接信息"))

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

	bus.KV(i18n.Text("Client", "客户端"), fmt.Sprintf("%s  (%s)", clientIP, clientISP))
	bus.KV("  ASN", clientAS)
	bus.KV(i18n.Text("  Location", "  位置"), clientLoc)

	serverIP := ep.IP
	if serverIP == "" && host != "" {
		// DNS fallback: resolve host to enrich server metadata
		serverIP = endpoint.ResolveHost(host)
	}
	if serverIP == "" {
		serverIP = "?"
		ok = false
	}
	bus.KV(i18n.Text("Server", "服务端"), fmt.Sprintf("%s  \u2192  %s", host, serverIP))
	if ep.Desc != "" {
		bus.KV(i18n.Text("  Endpoint", "  节点"), ep.Desc)
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
		bus.KV(i18n.Text("  Location", "  位置"), sLoc)
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
