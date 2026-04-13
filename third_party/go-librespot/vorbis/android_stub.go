//go:build android

package vorbis

import (
	"errors"
	"io"

	librespot "github.com/devgianlu/go-librespot"
)

var errVorbisUnsupportedOnAndroid = errors.New("vorbis decoder is not supported on android")

// Decoder is an Android placeholder to keep package APIs buildable.
type Decoder struct {
	SampleRate int32
	Channels   int32
}

type Info struct {
	Channels   int32
	SampleRate int32
	Comments   []string
	Vendor     string
}

type MetadataPage struct {
	trackGainDb   float32
	trackPeak     float32
	albumGainDb   float32
	albumPeak     float32
	hasReplayGain bool
}

func ExtractMetadataPage(log librespot.Logger, r io.ReaderAt, limit int64) (librespot.SizedReadAtSeeker, *MetadataPage, error) {
	if rr, ok := r.(librespot.SizedReadAtSeeker); ok {
		return rr, nil, nil
	}
	return nil, nil, errVorbisUnsupportedOnAndroid
}

func New(log librespot.Logger, r librespot.SizedReadAtSeeker, meta *MetadataPage, gain float32) (*Decoder, error) {
	return nil, errVorbisUnsupportedOnAndroid
}

func (d *Decoder) Close() {}

func (d *Decoder) Read(p []float32) (n int, err error) {
	return 0, io.EOF
}

func (d *Decoder) SetPositionMs(pos int64) error {
	return errVorbisUnsupportedOnAndroid
}

func (d *Decoder) PositionMs() int64 {
	return 0
}

