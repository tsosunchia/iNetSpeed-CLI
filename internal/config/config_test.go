package config

import (
	"errors"
	"os"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"2G", 2_000_000_000},
		{"2GB", 2_000_000_000},
		{"500M", 500_000_000},
		{"500MB", 500_000_000},
		{"1T", 1_000_000_000_000},
		{"1GiB", 1 << 30},
		{"1MiB", 1 << 20},
		{"1KiB", 1024},
		{"1TiB", 1 << 40},
		{"10K", 10_000},
		{"10KB", 10_000},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseSizeErrors(t *testing.T) {
	bads := []string{"", "abc", "2X", "-5G"}
	for _, s := range bads {
		_, err := ParseSize(s)
		if err == nil {
			t.Errorf("ParseSize(%q) expected error", s)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.00 GiB"},
	}
	for _, tt := range tests {
		got := HumanBytes(tt.input)
		if got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	// Clear all env vars
	for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
		os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DLURL != DefaultDLURL {
		t.Errorf("DLURL = %q, want %q", cfg.DLURL, DefaultDLURL)
	}
	if cfg.ULURL != DefaultULURL {
		t.Errorf("ULURL = %q, want %q", cfg.ULURL, DefaultULURL)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %d, want %d", cfg.Timeout, DefaultTimeout)
	}
	if cfg.Threads != DefaultThreads {
		t.Errorf("Threads = %d, want %d", cfg.Threads, DefaultThreads)
	}
	if cfg.LatencyCount != DefaultLatencyCount {
		t.Errorf("LatencyCount = %d, want %d", cfg.LatencyCount, DefaultLatencyCount)
	}
	if cfg.MaxBytes != 2_000_000_000 {
		t.Errorf("MaxBytes = %d, want 2000000000", cfg.MaxBytes)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	os.Setenv("DL_URL", "https://example.com/dl")
	os.Setenv("UL_URL", "https://example.com/ul")
	os.Setenv("TIMEOUT", "5")
	os.Setenv("THREADS", "8")
	os.Setenv("LATENCY_COUNT", "10")
	os.Setenv("MAX", "1G")
	defer func() {
		for _, k := range []string{"DL_URL", "UL_URL", "TIMEOUT", "THREADS", "LATENCY_COUNT", "MAX"} {
			os.Unsetenv(k)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DLURL != "https://example.com/dl" {
		t.Errorf("DLURL = %q", cfg.DLURL)
	}
	if cfg.Timeout != 5 {
		t.Errorf("Timeout = %d", cfg.Timeout)
	}
	if cfg.Threads != 8 {
		t.Errorf("Threads = %d", cfg.Threads)
	}
	if cfg.MaxBytes != 1_000_000_000 {
		t.Errorf("MaxBytes = %d", cfg.MaxBytes)
	}
}

func TestLoadInvalidParams(t *testing.T) {
	tests := []struct {
		key, val string
	}{
		{"MAX", "0"},
		{"MAX", "abc"},
		{"TIMEOUT", "0"},
		{"THREADS", "0"},
		{"LATENCY_COUNT", "0"},
		{"DL_URL", "not-a-url"},
	}
	for _, tt := range tests {
		// Reset all to valid defaults
		for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
			os.Unsetenv(k)
		}
		os.Setenv(tt.key, tt.val)
		_, err := Load()
		if err == nil {
			t.Errorf("Load() with %s=%q should fail", tt.key, tt.val)
		}
		os.Unsetenv(tt.key)
	}
}

func TestSummary(t *testing.T) {
	for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
		os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Summary()
	if s == "" {
		t.Error("Summary() is empty")
	}
}

func TestLoadUpperLimits(t *testing.T) {
	tests := []struct {
		key, val string
	}{
		{"TIMEOUT", "121"},
		{"THREADS", "65"},
		{"LATENCY_COUNT", "101"},
	}
	for _, tt := range tests {
		for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
			os.Unsetenv(k)
		}
		os.Setenv(tt.key, tt.val)
		_, err := Load()
		if err == nil {
			t.Errorf("Load() with %s=%q should fail (upper limit exceeded)", tt.key, tt.val)
		}
		os.Unsetenv(tt.key)
	}
}

func TestLoadUpperLimitsAtBoundary(t *testing.T) {
	for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
		os.Unsetenv(k)
	}
	os.Setenv("TIMEOUT", "120")
	os.Setenv("THREADS", "64")
	os.Setenv("LATENCY_COUNT", "100")
	defer func() {
		os.Unsetenv("TIMEOUT")
		os.Unsetenv("THREADS")
		os.Unsetenv("LATENCY_COUNT")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with boundary values should succeed: %v", err)
	}
	if cfg.Timeout != 120 || cfg.Threads != 64 || cfg.LatencyCount != 100 {
		t.Errorf("unexpected values: %+v", cfg)
	}
}

func TestLoadFlagOverrides(t *testing.T) {
	for _, k := range []string{"DL_URL", "UL_URL", "LATENCY_URL", "MAX", "TIMEOUT", "THREADS", "LATENCY_COUNT"} {
		os.Unsetenv(k)
	}
	os.Setenv("TIMEOUT", "5")
	defer os.Unsetenv("TIMEOUT")

	cfg, err := Load(
		"--dl-url", "https://example.com/dl2",
		"--ul-url", "https://example.com/ul2",
		"--latency-url", "https://example.com/la2",
		"--max", "3G",
		"--timeout", "12",
		"--threads", "9",
		"--latency-count", "15",
	)
	if err != nil {
		t.Fatalf("Load() with flags should succeed: %v", err)
	}

	if cfg.DLURL != "https://example.com/dl2" {
		t.Errorf("DLURL = %q", cfg.DLURL)
	}
	if cfg.ULURL != "https://example.com/ul2" {
		t.Errorf("ULURL = %q", cfg.ULURL)
	}
	if cfg.LatencyURL != "https://example.com/la2" {
		t.Errorf("LatencyURL = %q", cfg.LatencyURL)
	}
	if cfg.Max != "3G" || cfg.MaxBytes != 3_000_000_000 {
		t.Errorf("Max/MaxBytes = %q/%d", cfg.Max, cfg.MaxBytes)
	}
	if cfg.Timeout != 12 {
		t.Errorf("Timeout = %d", cfg.Timeout)
	}
	if cfg.Threads != 9 {
		t.Errorf("Threads = %d", cfg.Threads)
	}
	if cfg.LatencyCount != 15 {
		t.Errorf("LatencyCount = %d", cfg.LatencyCount)
	}
}

func TestLoadHelpRequested(t *testing.T) {
	tests := [][]string{
		{"help"},
		{"-h"},
		{"--help"},
	}
	for _, args := range tests {
		_, err := Load(args...)
		if !errors.Is(err, ErrHelp) {
			t.Fatalf("Load(%v) = %v, want ErrHelp", args, err)
		}
	}
}

func TestLoadUnexpectedArgs(t *testing.T) {
	_, err := Load("extra")
	if err == nil {
		t.Fatal("Load() with unexpected args should fail")
	}
}
