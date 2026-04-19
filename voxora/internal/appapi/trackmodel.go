package appapi

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"sync"

	libspotdl "github.com/McMelonTV/Voxora/libspotd"
)

// TrackRow is a normalized, bridge-friendly track projection.
type TrackRow struct {
	Name           string `json:"Name"`
	URI            string `json:"URI"`
	ArtistText     string `json:"ArtistText"`
	AlbumArtURL    string `json:"AlbumArtURL"`
	DownloadedPath string `json:"DownloadedPath"`
	DurationMs     int    `json:"DurationMs"`
}

// TrackModelSnapshot exposes additive metadata for bridge consumers.
type TrackModelSnapshot struct {
	Version int `json:"version"`
	Count   int `json:"count"`
}

// TrackModelManager tracks normalized rows and version progression.
type TrackModelManager struct {
	mu          sync.RWMutex
	version     int
	fingerprint uint64
	rows        []TrackRow
}

func NewTrackModelManager() *TrackModelManager {
	return &TrackModelManager{}
}

func (m *TrackModelManager) Reset() TrackModelSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = nil
	m.fingerprint = 0
	m.version++
	return TrackModelSnapshot{Version: m.version, Count: 0}
}

func (m *TrackModelManager) Snapshot() TrackModelSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return TrackModelSnapshot{Version: m.version, Count: len(m.rows)}
}

func (m *TrackModelManager) Rows() []TrackRow {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TrackRow, len(m.rows))
	copy(out, m.rows)
	return out
}

func (m *TrackModelManager) UpdateFromSummaries(items []libspotdl.LibraryTrackSummary) TrackModelSnapshot {
	normalized := normalizeSummaries(items)
	nextFingerprint := fingerprintRows(normalized)

	m.mu.Lock()
	defer m.mu.Unlock()

	if nextFingerprint != m.fingerprint {
		m.rows = normalized
		m.fingerprint = nextFingerprint
		m.version++
	}

	return TrackModelSnapshot{Version: m.version, Count: len(m.rows)}
}

func normalizeSummaries(items []libspotdl.LibraryTrackSummary) []TrackRow {
	if len(items) == 0 {
		return nil
	}
	out := make([]TrackRow, 0, len(items))
	for _, item := range items {
		durationMs := int64(item.DurationMs)
		if durationMs < 0 {
			durationMs = 0
		}
		if durationMs > math.MaxInt {
			durationMs = math.MaxInt
		}
		row := TrackRow{
			Name:           strings.TrimSpace(item.Name),
			URI:            strings.TrimSpace(item.URI),
			ArtistText:     strings.TrimSpace(item.ArtistText),
			AlbumArtURL:    strings.TrimSpace(item.AlbumArtURL),
			DownloadedPath: strings.TrimSpace(item.DownloadedPath),
			DurationMs:     int(durationMs),
		}
		out = append(out, row)
	}
	return out
}

func fingerprintRows(rows []TrackRow) uint64 {
	h := fnv.New64a()
	for i, row := range rows {
		_, _ = h.Write([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s|%d\n",
			i,
			row.URI,
			row.Name,
			row.ArtistText,
			row.AlbumArtURL,
			row.DownloadedPath,
			row.DurationMs,
		)))
	}
	return h.Sum64()
}
