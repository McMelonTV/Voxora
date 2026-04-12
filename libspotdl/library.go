package libspotdl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	extmetadatapb "github.com/devgianlu/go-librespot/proto/spotify/extendedmetadata"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	playlist4pb "github.com/devgianlu/go-librespot/proto/spotify/playlist4"
	librespotsession "github.com/devgianlu/go-librespot/session"
	libspotspclient "github.com/devgianlu/go-librespot/spclient"
	"google.golang.org/protobuf/proto"
)

// FetchLibrarySnapshot loads playlists and liked songs summary from one
// authenticated session.
func FetchLibrarySnapshot(ctx context.Context, cfg Config) (LibrarySnapshot, error) {
	return withInternalLibrarySession(ctx, cfg, func(ctx context.Context, sess *librespotsession.Session) (LibrarySnapshot, error) {
		playlists, err := fetchUserPlaylistsWithInternal(ctx, sess)
		if err != nil {
			return LibrarySnapshot{}, err
		}

		snapshot := LibrarySnapshot{
			Playlists:      playlists,
			Liked:          LikedSongsSummary{Name: "Liked Songs"},
			LikedAvailable: false,
		}

		liked, err := fetchLikedSongsSummaryWithInternal(ctx, sess)
		if err != nil {
			return snapshot, nil
		}

		snapshot.Liked = liked
		snapshot.LikedAvailable = true
		return snapshot, nil
	})
}

// FetchUserPlaylists loads all playlists owned/followed by the authenticated user.
func FetchUserPlaylists(ctx context.Context, cfg Config) ([]UserPlaylistSummary, error) {
	snapshot, err := FetchLibrarySnapshot(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return snapshot.Playlists, nil
}

// FetchLikedSongsSummary loads the authenticated user's "Liked Songs" collection summary.
func FetchLikedSongsSummary(ctx context.Context, cfg Config) (LikedSongsSummary, error) {
	snapshot, err := FetchLibrarySnapshot(ctx, cfg)
	if err != nil {
		return LikedSongsSummary{}, err
	}
	return snapshot.Liked, nil
}

// FetchContextTrackSummaries resolves all tracks under a context URI (for
// example a playlist URI or spotify:collection:tracks) and returns display
// summaries for UI lists.
func FetchContextTrackSummaries(ctx context.Context, cfg Config, contextURI string) ([]LibraryTrackSummary, error) {
	contextURI = strings.TrimSpace(contextURI)
	if contextURI == "" {
		return nil, errors.New("empty context uri")
	}

	offset := 0
	limit := 80
	out := make([]LibraryTrackSummary, 0, 256)
	for {
		page, err := FetchContextTrackSummariesPage(ctx, cfg, contextURI, offset, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)

		if !page.HasMore {
			break
		}
		offset += len(page.Items)
	}

	return out, nil
}

// FetchContextTrackSummariesPage resolves a page of track summaries under a
// context URI (playlist URI or spotify:collection:tracks).
func FetchContextTrackSummariesPage(ctx context.Context, cfg Config, contextURI string, offset, limit int) (LibraryTrackPage, error) {
	contextURI = strings.TrimSpace(contextURI)
	if contextURI == "" {
		return LibraryTrackPage{}, errors.New("empty context uri")
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 80
	}

	return withInternalLibrarySession(ctx, cfg, func(ctx context.Context, sess *librespotsession.Session) (LibraryTrackPage, error) {
		return fetchContextTrackSummariesPageWithInternal(ctx, sess, contextURI, offset, limit)
	})
}

func fetchContextTrackSummariesPageWithInternal(ctx context.Context, sess *librespotsession.Session, contextURI string, offset, limit int) (LibraryTrackPage, error) {
	resolved, err := sess.Spclient().ContextResolve(ctx, contextURI)
	if err != nil {
		return LibraryTrackPage{}, fmt.Errorf("resolve context %s: %w", contextURI, err)
	}

	resolver, err := libspotspclient.NewContextResolver(ctx, &librespot.NullLogger{}, sess.Spclient(), resolved)
	if err != nil {
		return LibraryTrackPage{}, fmt.Errorf("initialize context resolver: %w", err)
	}

	selectedURIs := make([]string, 0, limit)
	totalTracks := 0

	for pageIdx := 0; ; pageIdx++ {
		pageTracks, pageErr := resolver.Page(ctx, pageIdx)
		if errors.Is(pageErr, io.EOF) {
			break
		}
		if pageErr != nil {
			return LibraryTrackPage{}, fmt.Errorf("load context page %d: %w", pageIdx, pageErr)
		}

		for _, tr := range pageTracks {
			uri := strings.TrimSpace(tr.Uri)
			if !strings.HasPrefix(uri, "spotify:track:") {
				continue
			}

			if totalTracks >= offset && len(selectedURIs) < limit {
				selectedURIs = append(selectedURIs, uri)
			}
			totalTracks++
		}
	}

	if len(selectedURIs) == 0 {
		return LibraryTrackPage{Items: []LibraryTrackSummary{}, Offset: offset, Limit: limit, Total: totalTracks, HasMore: false}, nil
	}

	resolvedSummaries, err := resolveTrackSummariesBatch(ctx, sess, selectedURIs)
	if err != nil {
		return LibraryTrackPage{}, err
	}

	items := make([]LibraryTrackSummary, 0, len(selectedURIs))
	for _, uri := range selectedURIs {
		if summary, ok := resolvedSummaries[uri]; ok {
			items = append(items, summary)
			continue
		}
		items = append(items, LibraryTrackSummary{URI: uri, Name: uri})
	}

	return LibraryTrackPage{
		Items:   items,
		Offset:  offset,
		Limit:   limit,
		Total:   totalTracks,
		HasMore: offset+len(items) < totalTracks,
	}, nil
}

func resolveTrackSummariesBatch(ctx context.Context, sess *librespotsession.Session, uris []string) (map[string]LibraryTrackSummary, error) {
	entityReq := make([]*extmetadatapb.EntityRequest, 0, len(uris))
	for _, uri := range uris {
		entityReq = append(entityReq, &extmetadatapb.EntityRequest{
			EntityUri: uri,
			Query: []*extmetadatapb.ExtensionQuery{{
				ExtensionKind: extmetadatapb.ExtensionKind_TRACK_V4,
			}},
		})
	}

	resp, err := sess.Spclient().ExtendedMetadata(ctx, &extmetadatapb.BatchedEntityRequest{EntityRequest: entityReq})
	if err != nil {
		return nil, fmt.Errorf("batch resolve track metadata: %w", err)
	}

	summaries := make(map[string]LibraryTrackSummary, len(uris))
	for _, uri := range uris {
		summaries[uri] = LibraryTrackSummary{URI: uri, Name: uri}
	}

	for _, group := range resp.GetExtendedMetadata() {
		if group.GetExtensionKind() != extmetadatapb.ExtensionKind_TRACK_V4 {
			continue
		}

		for _, extData := range group.GetExtensionData() {
			uri := strings.TrimSpace(extData.GetEntityUri())
			if uri == "" {
				continue
			}

			summary, ok := summaries[uri]
			if !ok {
				continue
			}

			if extData.GetHeader() == nil || extData.GetHeader().GetStatusCode() != 200 {
				continue
			}

			var meta metadatapb.Track
			if extData.GetExtensionData() == nil {
				continue
			}
			if err := extData.GetExtensionData().UnmarshalTo(&meta); err != nil {
				continue
			}

			name := strings.TrimSpace(meta.GetName())
			if name != "" {
				summary.Name = name
			}

			artists := make([]string, 0, len(meta.GetArtist()))
			for _, a := range meta.GetArtist() {
				artistName := strings.TrimSpace(a.GetName())
				if artistName != "" {
					artists = append(artists, artistName)
				}
			}
			summary.ArtistText = strings.Join(artists, ", ")
			summary.DurationMs = int64(meta.GetDuration())
			summaries[uri] = summary
		}
	}

	return summaries, nil
}

func withInternalLibrarySession[T any](ctx context.Context, cfg Config, fn func(context.Context, *librespotsession.Session) (T, error)) (T, error) {
	var zero T

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	log := cfg.Logger
	if log == nil {
		log = newDefaultLogger()
	}

	sess, err := newAuthenticatedSession(ctx, cfg, log, client)
	if err != nil {
		return zero, err
	}
	defer sess.Close()

	return fn(ctx, sess)
}

func fetchUserPlaylistsWithInternal(ctx context.Context, sess *librespotsession.Session) ([]UserPlaylistSummary, error) {
	username := strings.TrimSpace(sess.Username())
	if username == "" {
		return nil, errors.New("spotify session username is empty")
	}

	rootlistPath := fmt.Sprintf("/playlist/v2/user/%s/rootlist", username)
	status, raw, err := spclientRequest(ctx, sess, http.MethodGet, rootlistPath)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("spotify internal rootlist request failed with status %d", status)
	}

	var rootlist playlist4pb.SelectedListContent
	if err := proto.Unmarshal(raw, &rootlist); err != nil {
		return nil, fmt.Errorf("decode spotify rootlist response: %w", err)
	}

	out := make([]UserPlaylistSummary, 0, 64)
	seen := make(map[string]struct{}, len(rootlist.GetContents().GetItems()))
	for _, item := range rootlist.GetContents().GetItems() {
		uri := strings.TrimSpace(item.GetUri())
		if !strings.HasPrefix(uri, "spotify:playlist:") {
			continue
		}
		if _, exists := seen[uri]; exists {
			continue
		}
		seen[uri] = struct{}{}

		playlistID := strings.TrimSpace(strings.TrimPrefix(uri, "spotify:playlist:"))
		if playlistID == "" {
			continue
		}

		metaPath := fmt.Sprintf("/playlist/v2/playlist/%s/metadata", playlistID)
		metaStatus, metaRaw, err := spclientRequest(ctx, sess, http.MethodGet, metaPath)
		if err != nil {
			continue
		}
		if metaStatus != http.StatusOK {
			continue
		}

		var meta playlist4pb.SelectedListContent
		if err := proto.Unmarshal(metaRaw, &meta); err != nil {
			continue
		}

		name := strings.TrimSpace(meta.GetAttributes().GetName())
		if name == "" {
			name = uri
		}

		out = append(out, UserPlaylistSummary{
			ID:          playlistID,
			URI:         uri,
			Name:        name,
			Description: strings.TrimSpace(meta.GetAttributes().GetDescription()),
			OwnerName:   strings.TrimSpace(meta.GetOwnerUsername()),
			TrackCount:  int(meta.GetLength()),
			ImageURL:    "",
			Public:      item.GetAttributes().GetPublic(),
		})
	}

	return out, nil
}

func fetchLikedSongsSummaryWithInternal(ctx context.Context, sess *librespotsession.Session) (LikedSongsSummary, error) {
	collectionCtx, err := sess.Spclient().ContextResolve(ctx, "spotify:collection:tracks")
	if err != nil {
		return LikedSongsSummary{}, fmt.Errorf("resolve collection context: %w", err)
	}

	resolver, err := libspotspclient.NewContextResolver(ctx, &librespot.NullLogger{}, sess.Spclient(), collectionCtx)
	if err != nil {
		return LikedSongsSummary{}, fmt.Errorf("initialize collection resolver: %w", err)
	}

	trackCount := 0
	for pageIdx := 0; ; pageIdx++ {
		tracks, pageErr := resolver.Page(ctx, pageIdx)
		if errors.Is(pageErr, io.EOF) {
			break
		}
		if pageErr != nil {
			return LikedSongsSummary{}, fmt.Errorf("load collection page %d: %w", pageIdx, pageErr)
		}
		trackCount += len(tracks)
	}

	return LikedSongsSummary{
		URI:        "spotify:collection:tracks",
		Name:       "Liked Songs",
		TrackCount: trackCount,
	}, nil
}

func spclientRequest(ctx context.Context, sess *librespotsession.Session, method, path string) (int, []byte, error) {
	resp, err := sess.Spclient().Request(ctx, method, path, nil, nil, nil)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read spclient response: %w", err)
	}

	return resp.StatusCode, body, nil
}
