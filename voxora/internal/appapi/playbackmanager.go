package appapi

import (
	"fmt"
)

// PlaybackIntent is a Go-selected playback decision payload consumed by QML.
type PlaybackIntent struct {
	Index          int    `json:"Index"`
	ShouldPlay     bool   `json:"ShouldPlay"`
	Name           string `json:"Name"`
	URI            string `json:"URI"`
	ArtistText     string `json:"ArtistText"`
	AlbumArtURL    string `json:"AlbumArtURL"`
	DownloadedPath string `json:"DownloadedPath"`
	DurationMs     int    `json:"DurationMs"`
}

// PlaybackManager owns non-visual playback selection decisions.
type PlaybackManager struct{}

func NewPlaybackManager() *PlaybackManager {
	return &PlaybackManager{}
}

func (m *PlaybackManager) StartAtIndex(rows []TrackRow, index int, shouldPlay bool) (PlaybackIntent, error) {
	if index < 0 || index >= len(rows) {
		return PlaybackIntent{}, fmt.Errorf("index out of range: %d", index)
	}
	row := rows[index]
	if row.URI == "" {
		return PlaybackIntent{}, fmt.Errorf("track has empty uri at index %d", index)
	}
	return PlaybackIntent{
		Index:          index,
		ShouldPlay:     shouldPlay,
		Name:           row.Name,
		URI:            row.URI,
		ArtistText:     row.ArtistText,
		AlbumArtURL:    row.AlbumArtURL,
		DownloadedPath: row.DownloadedPath,
		DurationMs:     row.DurationMs,
	}, nil
}

func (m *PlaybackManager) Adjacent(rows []TrackRow, currentIndex int, step int, shouldPlay bool) (PlaybackIntent, error) {
	if len(rows) == 0 {
		return PlaybackIntent{}, fmt.Errorf("track list is empty")
	}
	start := currentIndex
	if start < 0 {
		start = 0
	}
	next := start + step
	if next < 0 || next >= len(rows) {
		return PlaybackIntent{}, fmt.Errorf("adjacent index out of range: %d", next)
	}
	return m.StartAtIndex(rows, next, shouldPlay)
}

// ComputePrebufferQueue returns URIs for upcoming tracks that should be prefetched.
// Policy mirrors existing QML behavior: ahead window only, skip downloaded tracks,
// skip empty URIs, stop at list boundary.
func (m *PlaybackManager) ComputePrebufferQueue(rows []TrackRow, currentIndex int, aheadCount int) []string {
	if len(rows) == 0 || currentIndex < 0 || currentIndex >= len(rows) {
		return nil
	}
	maxAhead := aheadCount
	if maxAhead < 0 {
		maxAhead = 0
	}

	out := make([]string, 0, maxAhead)
	for step := 1; step <= maxAhead; step++ {
		nextIndex := currentIndex + step
		if nextIndex < 0 || nextIndex >= len(rows) {
			break
		}
		next := rows[nextIndex]
		if next.DownloadedPath != "" {
			continue
		}
		if next.URI == "" {
			continue
		}
		out = append(out, next.URI)
	}

	return out
}
