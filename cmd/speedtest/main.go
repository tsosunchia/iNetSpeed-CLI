package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsosunchia/iNetSpeed-CLI/internal/config"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/i18n"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/render"
	"github.com/tsosunchia/iNetSpeed-CLI/internal/runner"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	i18n.SetFromEnv()
	if lang, ok := i18n.FindLangArg(os.Args[1:]); ok {
		i18n.Set(lang)
	}

	if isVersionRequest(os.Args[1:]) {
		fmt.Printf(i18n.Text("speedtest %s (commit %s, built %s)\n", "speedtest %s（commit %s，构建于 %s）\n"), version, commit, date)
		os.Exit(0)
	}

	cfg, err := config.Load(os.Args[1:]...)
	if err != nil {
		if errors.Is(err, config.ErrHelp) {
			fmt.Print(config.Usage())
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "  [\u2717] %s\n", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprint(os.Stderr, config.Usage())
		os.Exit(1)
	}

	var r render.Renderer
	isTTY := render.IsTTY()
	if isTTY {
		r = render.NewTTYRenderer()
	} else {
		r = render.NewPlainRenderer(os.Stderr)
	}

	bus := render.NewBus(r)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	exitCode := runner.Run(ctx, cfg, bus, isTTY)
	bus.Close()
	os.Exit(exitCode)
}

func isVersionRequest(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || arg == "--version" || arg == "version" {
			return true
		}
	}
	return false
}
