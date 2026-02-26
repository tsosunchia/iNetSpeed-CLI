package i18n

import "testing"

func TestResolve(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	if got := Resolve("zh-CN"); got != LangZH {
		t.Fatalf("Resolve(zh-CN) = %q, want %q", got, LangZH)
	}
	if got := Resolve("fr_FR"); got != LangEN {
		t.Fatalf("Resolve(fr_FR) = %q, want %q", got, LangEN)
	}
}

func TestDetectFromEnv(t *testing.T) {
	t.Setenv("SPEEDTEST_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANGUAGE", "")
	t.Setenv("LANG", "zh_CN.UTF-8")

	if got := DetectFromEnv(); got != LangZH {
		t.Fatalf("DetectFromEnv() = %q, want %q", got, LangZH)
	}
}

func TestFindLangArg(t *testing.T) {
	if v, ok := FindLangArg([]string{"--lang", "zh"}); !ok || v != "zh" {
		t.Fatalf("FindLangArg(--lang zh) = %q/%v, want zh/true", v, ok)
	}
	if v, ok := FindLangArg([]string{"--lang=en"}); !ok || v != "en" {
		t.Fatalf("FindLangArg(--lang=en) = %q/%v, want en/true", v, ok)
	}
	if _, ok := FindLangArg([]string{"--threads", "4"}); ok {
		t.Fatal("FindLangArg should not match unrelated args")
	}
}
