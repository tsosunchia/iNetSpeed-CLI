package netx

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

type Options struct {
	PinHost string
	PinIP   string
	Timeout time.Duration
}

func NewClient(opts Options) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if opts.PinHost != "" {
		tlsCfg.ServerName = opts.PinHost
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsCfg,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if opts.PinHost != "" && opts.PinIP != "" {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			if host == opts.PinHost {
				addr = net.JoinHostPort(opts.PinIP, port)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	}

	_ = http2.ConfigureTransport(transport)

	return &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
	}
}
