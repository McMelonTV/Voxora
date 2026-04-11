package libspotdl

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/tracks"
)

var spotifyURLRegexp = regexp.MustCompile(`https://open\.spotify\.com(?:/intl-[a-z]{2})?/([a-z]+)/([a-zA-Z0-9]{21,22})`)

func normalizeSpotifyInput(input string) (string, ItemKind, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", errors.New("spotify source is required")
	}

	if strings.HasPrefix(trimmed, "spotify:") {
		id, err := librespot.SpotifyIdFromUri(trimmed)
		if err != nil {
			return "", "", fmt.Errorf("parse spotify uri: %w", err)
		}
		kind, err := itemKindFromType(id.Type())
		if err != nil {
			return "", "", err
		}
		return id.Uri(), kind, nil
	}

	match := spotifyURLRegexp.FindStringSubmatch(trimmed)
	if len(match) != 3 {
		return "", "", fmt.Errorf("unsupported spotify input: %q", input)
	}

	kind, err := itemKindFromString(match[1])
	if err != nil {
		return "", "", err
	}

	id, err := librespot.SpotifyIdFromBase62(librespot.SpotifyIdType(match[1]), match[2])
	if err != nil {
		return "", "", fmt.Errorf("parse spotify url: %w", err)
	}

	return id.Uri(), kind, nil
}

func itemKindFromString(kind string) (ItemKind, error) {
	switch ItemKind(kind) {
	case ItemKindTrack, ItemKindEpisode, ItemKindAlbum, ItemKindPlaylist, ItemKindShow:
		return ItemKind(kind), nil
	default:
		return "", fmt.Errorf("unsupported spotify item type: %s", kind)
	}
}

func itemKindFromType(kind librespot.SpotifyIdType) (ItemKind, error) {
	return itemKindFromString(string(kind))
}

func (d *Downloader) Resolve(ctx context.Context, input string) ([]ResolvedItem, error) {
	uri, kind, err := normalizeSpotifyInput(input)
	if err != nil {
		return nil, err
	}

	switch kind {
	case ItemKindTrack, ItemKindEpisode:
		return []ResolvedItem{{URI: uri, Kind: kind, Index: 0}}, nil
	case ItemKindAlbum, ItemKindPlaylist, ItemKindShow:
		spotCtx, err := d.sess.Spclient().ContextResolve(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("resolve spotify context: %w", err)
		}

		list, err := tracks.NewTrackListFromContext(ctx, d.log, d.sess.Spclient(), spotCtx)
		if err != nil {
			return nil, fmt.Errorf("build track list from context: %w", err)
		}

		provided := list.AllTracks(ctx)
		items := make([]ResolvedItem, 0, len(provided))
		for _, current := range provided {
			if current == nil || strings.TrimSpace(current.Uri) == "" {
				continue
			}

			resolvedURI, resolvedKind, err := normalizeSpotifyInput(current.Uri)
			if err != nil {
				continue
			}
			if resolvedKind != ItemKindTrack && resolvedKind != ItemKindEpisode {
				continue
			}

			items = append(items, ResolvedItem{
				URI:   resolvedURI,
				Kind:  resolvedKind,
				Index: len(items),
			})
		}

		if len(items) == 0 {
			return nil, fmt.Errorf("no downloadable items found in %s", uri)
		}

		return items, nil
	default:
		return nil, fmt.Errorf("unsupported resolved item type: %s", kind)
	}
}
