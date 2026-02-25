package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tsosunchia/apple-cdn-network-bench/internal/config"
	"github.com/tsosunchia/apple-cdn-network-bench/internal/render"
	"github.com/tsosunchia/apple-cdn-network-bench/internal/runner"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("speedtest %s (commit %s, built %s)\n", version, commit, date)
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
