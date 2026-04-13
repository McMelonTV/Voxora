//go:build android

package flac

import (
	"errors"
	"io"

	librespot "github.com/devgianlu/go-librespot"
)

var errFlacUnsupportedOnAndroid = errors.New("flac decoder is not supported on android")

// Decoder is an Android placeholder to keep package APIs buildable.
type Decoder struct {
	SampleRate int32
	Channels   int32
}

func New(log librespot.Logger, r librespot.SizedReadAtSeeker, gain float32) (*Decoder, error) {
	return nil, errFlacUnsupportedOnAndroid
}

func (d *Decoder) Read(p []float32) (n int, err error) {
	return 0, io.EOF
}

func (d *Decoder) SetPositionMs(pos int64) error {
	return errFlacUnsupportedOnAndroid
}

func (d *Decoder) PositionMs() int64 {
	return 0
}

func (d *Decoder) Close() error {
	return nil
}


