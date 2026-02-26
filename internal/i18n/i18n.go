package i18n

import (
	"os"
	"strings"
	"sync/atomic"
)

const (
	LangEN = "en"
	LangZH = "zh"
)

var current atomic.Value

func init() {
	current.Store(LangEN)
}

func normalize(lang string) string {
	v := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(v, LangZH) {
		return LangZH
	}
	return LangEN
}

func DetectFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("SPEEDTEST_LANG")); v != "" {
		return normalize(v)
	}
	keys := []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"}
	for _, k := range keys {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(os.Getenv(k))), LangZH) {
			return LangZH
		}
	}
	return LangEN
}

func Resolve(override string) string {
	if strings.TrimSpace(override) != "" {
		return normalize(override)
	}
	return DetectFromEnv()
}

func Set(lang string) {
	current.Store(normalize(lang))
}

func SetFromEnv() {
	Set(DetectFromEnv())
}

func Lang() string {
	v := current.Load()
	if s, ok := v.(string); ok {
		return s
	}
	return LangEN
}

func IsZH() bool {
	return Lang() == LangZH
}

func Text(en, zh string) string {
	if IsZH() {
		return zh
	}
	return en
}

func FindLangArg(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "--lang" {
			if i+1 >= len(args) {
				return "", false
			}
			return strings.TrimSpace(args[i+1]), true
		}
		if strings.HasPrefix(arg, "--lang=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--lang=")), true
		}
	}
	return "", false
}
