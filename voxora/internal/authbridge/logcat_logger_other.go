//go:build !android

package authbridge

import (
	librespot "github.com/devgianlu/go-librespot"
)

func newBridgeLogger() librespot.Logger {
	return &librespot.NullLogger{}
}
