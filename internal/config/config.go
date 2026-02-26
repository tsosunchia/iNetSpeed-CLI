package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
)

const (
	DefaultDLURL        = "https://mensura.cdn-apple.com/api/v1/gm/large"
	DefaultULURL        = "https://mensura.cdn-apple.com/api/v1/gm/slurp"
	DefaultLatencyURL   = "https://mensura.cdn-apple.com/api/v1/gm/small"
	DefaultMax          = "2G"
	DefaultTimeout      = 10
	DefaultThreads      = 4
	DefaultLatencyCount = 20
	UserAgent           = "networkQuality/194.80.3 CFNetwork/3860.400.51 Darwin/25.3.0"
)

var ErrHelp = errors.New("help requested")

type Config struct {
	DLURL        string
	ULURL        string
	LatencyURL   string
	Max          string
	MaxBytes     int64
	Timeout      int
	Threads      int
	LatencyCount int
}

func Usage() string {
	if i18n.IsZH() {
		return fmt.Sprintf(`用法:
  speedtest [选项]
  speedtest help

选项:
  -h, --help                    显示帮助信息
  -v, --version                 显示版本
  --lang LANG                   输出语言：zh 显示中文，其他显示英文（默认读取 SPEEDTEST_LANG/LC_ALL/LC_MESSAGES/LANGUAGE/LANG）
  --dl-url URL                  下载测速地址（默认取 DL_URL 或 %q）
  --ul-url URL                  上传测速地址（默认取 UL_URL 或 %q）
  --latency-url URL             延迟测速地址（默认取 LATENCY_URL 或 %q）
  --max SIZE                    单线程流量上限，如 2G/500M/1GiB（默认取 MAX 或 %q）
  --timeout SECONDS             单线程超时（秒），范围 1-120（默认取 TIMEOUT 或 %d）
  --threads N                   并发线程数，范围 1-64（默认取 THREADS 或 %d）
  --latency-count N             延迟采样次数，范围 1-100（默认取 LATENCY_COUNT 或 %d）

环境变量:
  DL_URL, UL_URL, LATENCY_URL, MAX, TIMEOUT, THREADS, LATENCY_COUNT
  SPEEDTEST_LANG, LC_ALL, LC_MESSAGES, LANGUAGE, LANG
`, DefaultDLURL, DefaultULURL, DefaultLatencyURL, DefaultMax, DefaultTimeout, DefaultThreads, DefaultLatencyCount)
	}

	return fmt.Sprintf(`Usage:
  speedtest [options]
  speedtest help

Options:
  -h, --help                    Show this help message
  -v, --version                 Show version
  --lang LANG                   Output language: zh for Chinese, others for English (default from SPEEDTEST_LANG/LC_ALL/LC_MESSAGES/LANGUAGE/LANG)
  --dl-url URL                  Download test URL (default from DL_URL or %q)
  --ul-url URL                  Upload test URL (default from UL_URL or %q)
  --latency-url URL             Latency test URL (default from LATENCY_URL or %q)
  --max SIZE                    Per-thread transfer cap, e.g. 2G/500M/1GiB (default from MAX or %q)
  --timeout SECONDS             Per-thread timeout in seconds, 1-120 (default from TIMEOUT or %d)
  --threads N                   Concurrent threads, 1-64 (default from THREADS or %d)
  --latency-count N             Latency sample count, 1-100 (default from LATENCY_COUNT or %d)

Environment variables:
  DL_URL, UL_URL, LATENCY_URL, MAX, TIMEOUT, THREADS, LATENCY_COUNT
  SPEEDTEST_LANG, LC_ALL, LC_MESSAGES, LANGUAGE, LANG
`, DefaultDLURL, DefaultULURL, DefaultLatencyURL, DefaultMax, DefaultTimeout, DefaultThreads, DefaultLatencyCount)
}

func Load(args ...string) (*Config, error) {
	langValue := ""
	if v, ok := i18n.FindLangArg(args); ok {
		langValue = v
	}
	i18n.Set(i18n.Resolve(langValue))

	if len(args) == 1 && args[0] == "help" {
		return nil, ErrHelp
	}

	dlURL := envOr("DL_URL", DefaultDLURL)
	ulURL := envOr("UL_URL", DefaultULURL)
	latencyURL := envOr("LATENCY_URL", DefaultLatencyURL)
	maxValue := envOr("MAX", DefaultMax)
	timeout := envInt("TIMEOUT", DefaultTimeout)
	threads := envInt("THREADS", DefaultThreads)
	latencyCount := envInt("LATENCY_COUNT", DefaultLatencyCount)

	if len(args) > 0 {
		fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
		fs.SetOutput(io.Discard)

		help := false
		fs.BoolVar(&help, "h", false, "show help")
		fs.BoolVar(&help, "help", false, "show help")
		fs.StringVar(&langValue, "lang", langValue, "output language (zh or en)")
		fs.StringVar(&dlURL, "dl-url", dlURL, "download test URL")
		fs.StringVar(&ulURL, "ul-url", ulURL, "upload test URL")
		fs.StringVar(&latencyURL, "latency-url", latencyURL, "latency test URL")
		fs.StringVar(&maxValue, "max", maxValue, "per-thread transfer cap")
		fs.IntVar(&timeout, "timeout", timeout, "per-thread timeout in seconds")
		fs.IntVar(&threads, "threads", threads, "concurrent threads")
		fs.IntVar(&latencyCount, "latency-count", latencyCount, "latency sample count")

		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		i18n.Set(i18n.Resolve(langValue))
		if help {
			return nil, ErrHelp
		}
		if fs.NArg() > 0 {
			if i18n.IsZH() {
				return nil, fmt.Errorf("存在未识别参数: %s", strings.Join(fs.Args(), " "))
			}
			return nil, fmt.Errorf("unexpected argument(s): %s", strings.Join(fs.Args(), " "))
		}
	}

	c := &Config{
		DLURL:        dlURL,
		ULURL:        ulURL,
		LatencyURL:   latencyURL,
		Max:          maxValue,
		Timeout:      timeout,
		Threads:      threads,
		LatencyCount: latencyCount,
	}

	var err error
	c.MaxBytes, err = ParseSize(c.Max)
	if err != nil {
		if i18n.IsZH() {
			return nil, fmt.Errorf("MAX 值无效 %q: %w", c.Max, err)
		}
		return nil, fmt.Errorf("invalid MAX %q: %w", c.Max, err)
	}
	if c.MaxBytes <= 0 {
		return nil, errors.New(i18n.Text("MAX must be > 0", "MAX 必须大于 0"))
	}
	if c.Timeout <= 0 {
		return nil, errors.New(i18n.Text("TIMEOUT must be > 0", "TIMEOUT 必须大于 0"))
	}
	if c.Threads <= 0 {
		return nil, errors.New(i18n.Text("THREADS must be > 0", "THREADS 必须大于 0"))
	}
	if c.LatencyCount <= 0 {
		return nil, errors.New(i18n.Text("LATENCY_COUNT must be > 0", "LATENCY_COUNT 必须大于 0"))
	}
	if c.Timeout > 120 {
		return nil, errors.New(i18n.Text("TIMEOUT must be <= 120", "TIMEOUT 必须小于等于 120"))
	}
	if c.Threads > 64 {
		return nil, errors.New(i18n.Text("THREADS must be <= 64", "THREADS 必须小于等于 64"))
	}
	if c.LatencyCount > 100 {
		return nil, errors.New(i18n.Text("LATENCY_COUNT must be <= 100", "LATENCY_COUNT 必须小于等于 100"))
	}
	for _, u := range []struct{ name, val string }{
		{"DL_URL", c.DLURL},
		{"UL_URL", c.ULURL},
		{"LATENCY_URL", c.LatencyURL},
	} {
		if !strings.HasPrefix(u.val, "http://") && !strings.HasPrefix(u.val, "https://") {
			if i18n.IsZH() {
				return nil, fmt.Errorf("%s 必须以 http(s):// 开头", u.name)
			}
			return nil, fmt.Errorf("%s must start with http(s)://", u.name)
		}
	}
	return c, nil
}

func (c *Config) Summary() string {
	if i18n.IsZH() {
		return fmt.Sprintf("超时=%ds  上限=%s  线程=%d  延迟采样=%d",
			c.Timeout, c.Max, c.Threads, c.LatencyCount)
	}
	return fmt.Sprintf("timeout=%ds  max=%s  threads=%d  latency_count=%d",
		c.Timeout, c.Max, c.Threads, c.LatencyCount)
}

var sizeRe = regexp.MustCompile(`(?i)^\s*([\d.]+)\s*([a-z]*)\s*$`)

func ParseSize(s string) (int64, error) {
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("cannot parse size %q", s)
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	unit := m[2]
	if unit == "" {
		return int64(num), nil
	}
	mul := int64(1)
	switch strings.ToLower(unit) {
	case "k", "kb":
		mul = 1000
	case "m", "mb":
		mul = 1000 * 1000
	case "g", "gb":
		mul = 1000 * 1000 * 1000
	case "t", "tb":
		mul = 1000 * 1000 * 1000 * 1000
	case "kib":
		mul = 1024
	case "mib":
		mul = 1024 * 1024
	case "gib":
		mul = 1024 * 1024 * 1024
	case "tib":
		mul = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit %q", unit)
	}
	return int64(num * float64(mul)), nil
}

func HumanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
