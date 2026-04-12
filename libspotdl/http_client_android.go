//go:build android

package libspotdl

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

func defaultHTTPClient() *http.Client {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			var lastErr error

			// Bypass local Android stub resolvers that can refuse queries from app
			// processes; prefer direct TCP DNS upstreams.
			for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
				conn, err := dialer.DialContext(ctx, "tcp", server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}

			if strings.TrimSpace(address) != "" {
				conn, err := dialer.DialContext(ctx, "tcp", address)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}

			return nil, lastErr
		},
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  resolver,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}
