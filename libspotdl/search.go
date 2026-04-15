package libspotdl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type spotifySearchResponse struct {
	Tracks spotifySearchTrackPage `json:"tracks"`
}

type spotifySearchTrackPage struct {
	Items  []spotifySearchTrack `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
	Next   string               `json:"next"`
}

type spotifySearchTrack struct {
	URI        string                `json:"uri"`
	Name       string                `json:"name"`
	DurationMs int64                 `json:"duration_ms"`
	Artists    []spotifySearchArtist `json:"artists"`
	Album      spotifySearchAlbum    `json:"album"`
}

type spotifySearchArtist struct {
	Name string `json:"name"`
}

type spotifySearchAlbum struct {
	Images []spotifySearchImage `json:"images"`
}

type spotifySearchImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// SearchTrackSummariesPageWithDownloader searches Spotify tracks using the
// authenticated downloader session and returns a page of compact display models.
func SearchTrackSummariesPageWithDownloader(ctx context.Context, d *Downloader, query string, offset, limit int) (LibraryTrackPage, error) {
	if d == nil || d.sess == nil {
		return LibraryTrackPage{}, fmt.Errorf("search spotify tracks: downloader session is not initialized")
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return LibraryTrackPage{}, fmt.Errorf("search spotify tracks: query is required")
	}

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 10 {
		limit = 10
	}

	params := url.Values{}
	params.Set("q", trimmedQuery)
	params.Set("type", string(ItemKindTrack))
	params.Set("offset", fmt.Sprintf("%d", offset))
	params.Set("limit", fmt.Sprintf("%d", limit))
	if market := strings.TrimSpace(d.countryCode); market != "" {
		params.Set("market", market)
	}

	resp, err := d.sess.WebApi(ctx, http.MethodGet, "/v1/search", params, nil, nil)
	if err != nil {
		return LibraryTrackPage{}, fmt.Errorf("search spotify tracks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return LibraryTrackPage{}, fmt.Errorf("spotify web api /v1/search returned status %d", resp.StatusCode)
	}

	var payload spotifySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return LibraryTrackPage{}, fmt.Errorf("decode spotify search response: %w", err)
	}

	items := make([]LibraryTrackSummary, 0, len(payload.Tracks.Items))
	for _, track := range payload.Tracks.Items {
		uri := strings.TrimSpace(track.URI)
		if uri == "" {
			continue
		}

		artistNames := make([]string, 0, len(track.Artists))
		for _, artist := range track.Artists {
			name := strings.TrimSpace(artist.Name)
			if name != "" {
				artistNames = append(artistNames, name)
			}
		}

		items = append(items, LibraryTrackSummary{
			URI:         uri,
			Name:        strings.TrimSpace(track.Name),
			ArtistText:  strings.Join(artistNames, ", "),
			AlbumArtURL: bestSpotifySearchImageURL(track.Album.Images),
			DurationMs:  track.DurationMs,
		})
	}

	pageOffset := payload.Tracks.Offset
	pageLimit := payload.Tracks.Limit
	if pageLimit <= 0 {
		pageLimit = limit
	}
	pageTotal := payload.Tracks.Total
	hasMore := strings.TrimSpace(payload.Tracks.Next) != ""
	if !hasMore && pageTotal > 0 {
		hasMore = pageOffset+len(items) < pageTotal
	}

	return LibraryTrackPage{
		Items:   items,
		Offset:  pageOffset,
		Limit:   pageLimit,
		Total:   pageTotal,
		HasMore: hasMore,
	}, nil
}

func bestSpotifySearchImageURL(images []spotifySearchImage) string {
	bestArea := -1
	best := ""
	for _, image := range images {
		candidate := strings.TrimSpace(image.URL)
		if candidate == "" {
			continue
		}
		area := image.Width * image.Height
		if area > bestArea {
			bestArea = area
			best = candidate
		}
	}
	if best == "" && len(images) > 0 {
		return strings.TrimSpace(images[0].URL)
	}
	return best
}
