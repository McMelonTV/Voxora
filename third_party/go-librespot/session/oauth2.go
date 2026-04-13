package session

import (
	"context"
	"fmt"
	"net"
	"net/http"

	librespot "github.com/devgianlu/go-librespot"
)

func NewOAuth2Server(ctx context.Context, log librespot.Logger, callbackPort int) (int, chan string, error) {
	// Spotify OAuth callback URL in this project uses 127.0.0.1. On Android,
	// binding ":port" may resolve to an IPv6-only socket, which breaks that
	// IPv4 loopback redirect.
	lis, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		// Fallback for non-Android/legacy environments where explicit loopback
		// bind may fail unexpectedly.
		lis, err = net.Listen("tcp", fmt.Sprintf(":%d", callbackPort))
	}
	if err != nil {
		return 0, nil, fmt.Errorf("failed to listen: %w", err)
	}

	errCh := make(chan error, 1)
	resCh := make(chan string, 1)
	go func() {
		errCh <- http.Serve(lis, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			resCh <- r.URL.Query().Get("code")
			_, _ = rw.Write([]byte("Go back to go-librespot!"))
		}))
	}()

	go func() {
		select {
		case <-ctx.Done():
			_ = lis.Close()
		case err := <-errCh:
			if err != nil {
				log.WithError(err).Errorf("failed service oauth2 server")
				resCh <- ""
			}
		}
	}()

	return lis.Addr().(*net.TCPAddr).Port, resCh, nil
}
