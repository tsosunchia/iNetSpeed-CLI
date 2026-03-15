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

func Run(ctx context.Context, cfg *config.Config, bus *render.Bus, isTTY bool) RunResult {
	started := time.Now()
	result := RunResult{
		SchemaVersion: 1,
		Config: RunConfig{
			DLURL:          cfg.DLURL,
			ULURL:          cfg.ULURL,
			LatencyURL:     cfg.LatencyURL,
			Max:            cfg.Max,
			MaxBytes:       cfg.MaxBytes,
			TimeoutSeconds: cfg.Timeout,
			Threads:        cfg.Threads,
			LatencyCount:   cfg.LatencyCount,
			JSON:           cfg.OutputJSON,
			NonInteractive: cfg.NonInteractive,
			EndpointIP:     cfg.EndpointIP,
			Metadata:       !cfg.NoMetadata,
		},
		SelectedEndpoint: SelectedEndpoint{Status: "unavailable"},
		ConnectionInfo: ConnectionInfo{
			Status:          "unavailable",
			MetadataEnabled: !cfg.NoMetadata,
			Client:          PeerInfo{Status: "unavailable"},
			Server:          PeerInfo{Status: "unavailable"},
		},
		IdleLatency: LatencyResult{Status: "unavailable"},
		StartedAt:   started.UTC().Format(time.RFC3339Nano),
	}

	if bus != nil {
		bus.Line()
		bus.Banner("\u26a1 iNetSpeed-CLI")
		bus.Info(i18n.Text("Config:  ", "配置:  ") + cfg.Summary())
		bus.Line()
		bus.Header(i18n.Text("Environment Check", "环境检查"))
		bus.Info(i18n.Text("Go binary — no external dependencies required.", "Go 二进制程序 — 无需外部依赖。"))
	}

	if interrupted(ctx) {
		return finalizeResult(started, result, 130)
	}

	dlHost := endpoint.HostFromURL(cfg.DLURL)
	ulHost := endpoint.HostFromURL(cfg.ULURL)
	latencyHost := endpoint.HostFromURL(cfg.LatencyURL)
	hostsConsistent := dlHost != "" && dlHost == ulHost && dlHost == latencyHost

	discovery := endpoint.DiscoveryResult{
		Host:       dlHost,
		Selected:   endpoint.Endpoint{Source: "default_dns", Status: "degraded"},
		DefaultDNS: true,
	}
	if hostsConsistent {
		discovery = endpoint.Discover(ctx, dlHost, endpoint.DiscoveryOptions{
			ProbeURL:   cfg.LatencyURL,
			EndpointIP: cfg.EndpointIP,
			Metadata:   !cfg.NoMetadata,
		})
	} else {
		result.Degraded = true
		addWarning(&result, "mixed_hosts", i18n.Text(
			"DL_URL, UL_URL and LATENCY_URL hosts differ. Shared endpoint pinning is disabled.",
			"DL_URL、UL_URL 与 LATENCY_URL 的主机不一致，已禁用共享节点固定。",
		))
		if cfg.EndpointIP != "" {
			addWarning(&result, "endpoint_ignored", i18n.Text(
				"--endpoint is ignored when test hosts differ.",
				"测速主机不一致时将忽略 --endpoint。",
			))
		}
	}
	for _, warning := range discovery.Warnings {
		addWarning(&result, warning.Code, warning.Message)
	}
	result.Candidates = candidateResults(discovery.Candidates)
	result.SelectedEndpoint = selectedEndpoint(discovery.Selected)
	if discovery.Selected.Status == "degraded" || discovery.DefaultDNS {
		result.Degraded = true
	}

	if bus != nil {
		renderSelection(bus, ctx, &discovery, isTTY && !cfg.NonInteractive && !cfg.OutputJSON)
		result.SelectedEndpoint = selectedEndpoint(discovery.Selected)
	}
	if interrupted(ctx) {
		return finalizeResult(started, result, 130)
	}

	clientOpts := netx.Options{Timeout: time.Duration(cfg.Timeout+5) * time.Second}
	if hostsConsistent && discovery.Selected.IP != "" && !discovery.DefaultDNS {
		clientOpts.PinHost = dlHost
		clientOpts.PinIP = discovery.Selected.IP
	}
	client := netx.NewClient(clientOpts)

	result.ConnectionInfo = gatherInfo(ctx, !cfg.NoMetadata, dlHost, discovery.Selected)
	if !cfg.NoMetadata && result.ConnectionInfo.Status != "ok" {
		result.Degraded = true
	}
	if bus != nil {
		renderConnectionInfo(bus, result.ConnectionInfo, !cfg.NoMetadata)
	}
	if interrupted(ctx) {
		return finalizeResult(started, result, 130)
	}

	if bus != nil {
		bus.Header(i18n.Text("Idle Latency", "空载延迟"))
		bus.Info(fmt.Sprintf(i18n.Text("Samples: %d", "采样: %d"), cfg.LatencyCount))
	}
	idleStats := latency.MeasureIdle(ctx, client, cfg.LatencyURL, cfg.LatencyCount)
	result.IdleLatency = latencyResult(idleStats, i18n.Text("No latency samples collected.", "未采集到延迟样本。"))
	if result.IdleLatency.Status != "ok" {
		result.Degraded = true
	}
	if bus != nil {
		renderLatency(bus, result.IdleLatency)
	}

	runRound := func(dir transfer.Direction, threads int, name string, url string) {
		if interrupted(ctx) {
			return
		}
		if bus != nil {
			bus.Header(name)
			bus.Info(fmt.Sprintf(i18n.Text("Threads: %d", "线程: %d"), threads))
			bus.Info(fmt.Sprintf(i18n.Text("Limit: %s / %ds per thread", "上限: %s / 每线程 %ds"), cfg.Max, cfg.Timeout))
		}

		loadedProbe := latency.StartLoaded(ctx, client, cfg.LatencyURL)
		res := transfer.Run(ctx, client, cfg, dir, threads, url, bus)
		loadedStats := loadedProbe.Stop()

		round := RoundResult{
			Name:          name,
			Direction:     directionName(dir),
			Threads:       threads,
			Status:        "ok",
			URL:           url,
			TotalBytes:    res.TotalBytes,
			DurationMs:    res.Duration.Milliseconds(),
			Mbps:          res.Mbps,
			FaultCount:    res.FaultCount,
			HadFault:      res.HadFault,
			LoadedLatency: latencyResult(loadedStats, i18n.Text("No loaded latency samples collected.", "未采集到负载延迟样本。")),
		}
		if res.HadFault {
			round.Status = "degraded"
			round.Error = i18n.Text("Network fault detected during transfer.", "传输过程中检测到网络故障。")
			result.Degraded = true
		}
		if res.TotalBytes == 0 {
			round.Status = "failed"
			if round.Error == "" {
				round.Error = i18n.Text("Transfer did not complete successfully.", "传输未成功完成。")
			}
			result.Degraded = true
		}
		if round.LoadedLatency.Status != "ok" && round.Status == "ok" {
			round.Status = "degraded"
			result.Degraded = true
		}

		result.TotalBytes += res.TotalBytes
		result.Rounds = append(result.Rounds, round)
		if bus != nil {
			renderRound(bus, round)
		}
	}

	runRound(transfer.Download, 1, i18n.Text("Download (single thread)", "下载（单线程）"), cfg.DLURL)
	if cfg.Threads > 1 {
		runRound(transfer.Download, cfg.Threads, i18n.Text("Download (multi-thread)", "下载（多线程）"), cfg.DLURL)
	}
	runRound(transfer.Upload, 1, i18n.Text("Upload (single thread)", "上传（单线程）"), cfg.ULURL)
	if cfg.Threads > 1 {
		runRound(transfer.Upload, cfg.Threads, i18n.Text("Upload (multi-thread)", "上传（多线程）"), cfg.ULURL)
	}

	if interrupted(ctx) {
		return finalizeResult(started, result, 130)
	}

	if bus != nil {
		renderSummary(bus, result)
	}
	exitCode := 0
	if result.Degraded {
		exitCode = 2
	}
	return finalizeResult(started, result, exitCode)
}

func finalizeResult(started time.Time, result RunResult, exitCode int) RunResult {
	result.ExitCode = exitCode
	result.DurationMs = time.Since(started).Milliseconds()
	if exitCode == 130 {
		result.Degraded = true
		addWarning(&result, "interrupted", i18n.Text("Interrupted.", "已中断。"))
	}
	return result
}

func renderSelection(bus *render.Bus, ctx context.Context, discovery *endpoint.DiscoveryResult, allowPrompt bool) {
	bus.Header(i18n.Text("Endpoint Selection", "节点选择"))
	if discovery.Host != "" {
		bus.Info(i18n.Text("Host: ", "主机: ") + discovery.Host)
	}
	for _, warning := range discovery.Warnings {
		bus.Warn(warning.Message)
	}
	if len(discovery.Candidates) == 0 {
		if discovery.Selected.IP == "" {
			bus.Warn(i18n.Text("Using default DNS without endpoint pinning.", "未固定节点，继续使用默认 DNS。"))
		}
		return
	}

	bus.Info(i18n.Text("Available endpoints:", "可用节点:"))
	for i, candidate := range discovery.Candidates {
		bus.Info(fmt.Sprintf("  %d) %s  %s", i+1, candidate.IP, endpoint.CandidateLabel(candidate)))
	}

	if len(discovery.Candidates) > 1 && allowPrompt {
		bus.Flush()
		choice, cancelled := endpoint.PromptChoice(ctx, len(discovery.Candidates), bus)
		if cancelled {
			discovery.Selected = endpoint.Endpoint{}
			return
		}
		selected := discovery.Candidates[choice]
		discovery.Selected = endpoint.Endpoint{
			IP:     selected.IP,
			Desc:   selected.Desc,
			RTTMs:  selected.RTTMs,
			Source: selected.Source,
			Status: selected.Status,
		}
	}
	if discovery.Selected.IP != "" {
		bus.Info(fmt.Sprintf(i18n.Text("Selected endpoint: %s (%s)", "已选择节点: %s (%s)"), discovery.Selected.IP, selectedDesc(discovery.Selected)))
	}
}

func renderConnectionInfo(bus *render.Bus, info ConnectionInfo, metadata bool) {
	bus.Header(i18n.Text("Connection Information", "连接信息"))
	if !metadata {
		bus.Info(i18n.Text("Metadata lookup disabled.", "已禁用元数据查询。"))
		return
	}
	renderPeer(bus, i18n.Text("Client", "客户端"), info.Client)
	renderPeer(bus, i18n.Text("Server", "服务端"), info.Server)
}

func renderPeer(bus *render.Bus, label string, peer PeerInfo) {
	bus.KV(label, fmt.Sprintf("%s  (%s)", fallback(peer.IP), fallback(peer.ISP)))
	bus.KV("  ASN", fallback(peer.ASN))
	bus.KV(i18n.Text("  Location", "  位置"), fallback(peer.Location))
}

func renderLatency(bus *render.Bus, result LatencyResult) {
	if result.Status != "ok" {
		bus.Warn(orFallback(result.Error, i18n.Text("Latency unavailable.", "延迟不可用。")))
		return
	}
	bus.Result(fmt.Sprintf(i18n.Text(
		"%.2f ms median  (min %.2f / avg %.2f / max %.2f)  jitter %.2f ms",
		"%.2f 毫秒 中位数  (最小 %.2f / 平均 %.2f / 最大 %.2f)  抖动 %.2f 毫秒"),
		value(result.MedianMs), value(result.MinMs), value(result.AvgMs), value(result.MaxMs), value(result.JitterMs)))
}

func renderRound(bus *render.Bus, round RoundResult) {
	if round.Threads <= 1 {
		bus.Result(fmt.Sprintf(i18n.Text("%.0f Mbps  (%s in %.1fs)", "%.0f Mbps  (%s，耗时 %.1fs)"),
			round.Mbps, config.HumanBytes(round.TotalBytes), float64(round.DurationMs)/1000))
	} else {
		bus.Result(fmt.Sprintf(i18n.Text("%.0f Mbps  (%s in %.1fs, %d threads)", "%.0f Mbps  (%s，耗时 %.1fs，%d 线程)"),
			round.Mbps, config.HumanBytes(round.TotalBytes), float64(round.DurationMs)/1000, round.Threads))
	}
	if round.Error != "" {
		bus.Warn(round.Error)
	}
	if round.LoadedLatency.Status == "ok" {
		bus.Info(fmt.Sprintf(i18n.Text("Loaded latency: %.2f ms  (jitter %.2f ms)", "负载延迟: %.2f 毫秒  (抖动 %.2f 毫秒)"),
			value(round.LoadedLatency.MedianMs), value(round.LoadedLatency.JitterMs)))
	} else {
		bus.Warn(orFallback(round.LoadedLatency.Error, i18n.Text("Loaded latency unavailable.", "负载延迟不可用。")))
	}
}

func renderSummary(bus *render.Bus, result RunResult) {
	bus.Line()
	bus.Banner(i18n.Text("\U0001f4ca Summary", "\U0001f4ca 测速汇总"))
	bus.Line()
	if result.IdleLatency.Status == "ok" {
		bus.KV(i18n.Text("Idle Latency", "空载延迟"), fmt.Sprintf(i18n.Text("%.2f ms  (jitter %.2f ms)", "%.2f 毫秒  (抖动 %.2f 毫秒)"),
			value(result.IdleLatency.MedianMs), value(result.IdleLatency.JitterMs)))
	} else {
		bus.KV(i18n.Text("Idle Latency", "空载延迟"), i18n.Text("unavailable", "不可用"))
	}
	bus.KV(i18n.Text("Data Used", "消耗流量"), config.HumanBytes(result.TotalBytes))
	bus.Line()
	if result.Degraded {
		bus.Warn(i18n.Text("Completed with degraded results.", "测速完成，但结果存在降级。"))
	} else {
		bus.Info(i18n.Text("All tests complete.", "所有测试完成。"))
	}
	bus.Line()
}

func gatherInfo(ctx context.Context, metadata bool, host string, selected endpoint.Endpoint) ConnectionInfo {
	info := ConnectionInfo{
		Status:          "ok",
		MetadataEnabled: metadata,
		Host:            host,
		Client:          PeerInfo{Status: "unavailable"},
		Server:          PeerInfo{Status: "unavailable"},
	}
	if !metadata {
		info.Status = "unavailable"
		return info
	}

	clientInfo := endpoint.FetchInfo(ctx, "")
	info.Client = peerFromInfo(clientInfo)
	if info.Client.Status != "ok" {
		info.Status = "degraded"
	}

	serverIP := selected.IP
	if serverIP == "" && host != "" {
		serverIP = endpoint.ResolveHost(host)
	}
	if serverIP != "" {
		serverInfo := endpoint.FetchInfo(ctx, serverIP)
		info.Server = peerFromInfo(serverInfo)
		if info.Server.IP == "" {
			info.Server.IP = serverIP
		}
		if info.Server.Status != "ok" {
			info.Status = "degraded"
		}
	} else {
		info.Server = PeerInfo{Status: "unavailable"}
		info.Status = "degraded"
	}
	return info
}

func peerFromInfo(info endpoint.IPInfo) PeerInfo {
	if info.Query == "" {
		return PeerInfo{Status: "unavailable"}
	}
	asn := info.AS
	if asn == "" {
		asn = info.Org
	}
	return PeerInfo{
		Status:   "ok",
		IP:       info.Query,
		ISP:      firstNonEmpty(info.ISP, info.Org),
		ASN:      firstNonEmpty(asn, i18n.Text("unavailable", "不可用")),
		Location: formatLocation(info),
	}
}

func latencyResult(stats latency.Stats, errMsg string) LatencyResult {
	if stats.N == 0 {
		return LatencyResult{Status: "unavailable", Error: errMsg}
	}
	return LatencyResult{
		Status:   "ok",
		Samples:  stats.N,
		MinMs:    floatPtr(stats.Min),
		AvgMs:    floatPtr(stats.Avg),
		MedianMs: floatPtr(stats.Median),
		MaxMs:    floatPtr(stats.Max),
		JitterMs: floatPtr(stats.Jitter),
	}
}

func candidateResults(candidates []endpoint.Candidate) []CandidateResult {
	out := make([]CandidateResult, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, CandidateResult{
			IP:          candidate.IP,
			Description: candidate.Desc,
			RTTMs:       floatPtrOrNil(candidate.RTTMs),
			Source:      candidate.Source,
			Status:      candidate.Status,
			Error:       candidate.Error,
		})
	}
	return out
}

func selectedEndpoint(selected endpoint.Endpoint) SelectedEndpoint {
	return SelectedEndpoint{
		IP:          selected.IP,
		Description: selected.Desc,
		RTTMs:       floatPtrOrNil(selected.RTTMs),
		Source:      selected.Source,
		Status:      firstNonEmpty(selected.Status, "unavailable"),
	}
}

func addWarning(result *RunResult, code, message string) {
	result.Warnings = append(result.Warnings, Warning{Code: code, Message: message})
}

func interrupted(ctx context.Context) bool {
	return ctx.Err() != nil
}

func directionName(direction transfer.Direction) string {
	if direction == transfer.Download {
		return "download"
	}
	return "upload"
}

func selectedDesc(selected endpoint.Endpoint) string {
	if selected.Desc != "" {
		return selected.Desc
	}
	return i18n.Text("unavailable", "不可用")
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
		return i18n.Text("unavailable", "不可用")
	}
	return loc
}

func floatPtr(v float64) *float64 {
	value := v
	return &value
}

func floatPtrOrNil(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	return floatPtr(v)
}

func value(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func fallback(v string) string {
	if v == "" {
		return i18n.Text("unavailable", "不可用")
	}
	return v
}

func orFallback(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
