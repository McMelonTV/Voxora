//go:build !android

package libspotdl

import (
	"net/http"
	"time"
)

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
