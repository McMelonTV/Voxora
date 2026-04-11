package authbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	libspotdl "github.com/McMelonTV/Voxora/libspotd"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

type SpotifyBridge struct {
	props *qml.QQmlPropertyMap

	mu         sync.Mutex
	flow       *libspotdl.InteractiveAuthFlow
	flowCtx    context.Context
	flowCancel context.CancelFunc

	trackContextURI string
	trackPageOffset int
	trackPageLimit  int
	trackItems      []libspotdl.LibraryTrackSummary
	loadingTracks   bool
}

func NewSpotifyBridge() *SpotifyBridge {
	props := qml.NewQQmlPropertyMap()
	b := &SpotifyBridge{props: props}

	b.set("state", "idle")
	b.set("statusText", "Spotify auth: idle")
	b.set("isBusy", false)
	b.set("username", "")
	b.set("lastError", "")
	b.set("authUrl", "")
	b.set("likedSongsCount", 0)
	b.set("likedSongsName", "Liked Songs")
	b.set("playlistsJson", "[]")
	b.set("playlistsCount", 0)
	b.set("libraryStatus", "Spotify library: not loaded")
	b.set("viewMode", "library")
	b.set("trackListJson", "[]")
	b.set("trackListTitle", "")
	b.set("trackListStatus", "")
	b.set("isLoadingTracks", false)
	b.set("trackHasMore", false)
	b.set("trackTotal", 0)
	b.set("isDownloadingTrack", false)
	b.set("downloadTrackRequest", "")
	b.set("openCollectionRequest", "")
	b.set("loadMoreTracksNonce", 0)
	b.set("navigateBackNonce", 0)
	b.set("startAuthNonce", 0)

	props.OnValueChanged(func(key string, value *qt.QVariant) {
		switch key {
		case "startAuthNonce":
			go b.startFlow()
		case "openCollectionRequest":
			go b.openCollection(value.ToString())
		case "loadMoreTracksNonce":
			go b.loadMoreTracks()
		case "downloadTrackRequest":
			go b.downloadTrack(value.ToString())
		case "navigateBackNonce":
			b.set("viewMode", "library")
			b.set("trackListStatus", "")
		}
	})

	go b.bootstrapExistingAuth()

	return b
}

func (b *SpotifyBridge) Object() *qml.QQmlPropertyMap {
	return b.props
}

func (b *SpotifyBridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.flow != nil {
		b.flow.Cancel()
		b.flow = nil
	}
	if b.flowCancel != nil {
		b.flowCancel()
		b.flowCancel = nil
		b.flowCtx = nil
	}
}

func (b *SpotifyBridge) startFlow() {
	b.mu.Lock()
	if b.flow != nil {
		b.mu.Unlock()
		return
	}
	flowCtx, flowCancel := context.WithTimeout(context.Background(), 12*time.Minute)
	b.flowCtx = flowCtx
	b.flowCancel = flowCancel
	b.mu.Unlock()

	b.set("isBusy", true)
	b.set("state", "pending")
	b.set("statusText", "Spotify auth: starting...")
	b.set("lastError", "")

	start, err := libspotdl.StartInteractiveAuthFlow(flowCtx, nil, 0)
	if err != nil {
		flowCancel()
		b.mu.Lock()
		b.flowCtx = nil
		b.flowCancel = nil
		b.mu.Unlock()
		b.set("isBusy", false)
		b.set("state", "error")
		b.set("lastError", err.Error())
		b.set("statusText", "Spotify auth: failed to start")
		return
	}

	b.mu.Lock()
	b.flow = start.Flow
	b.mu.Unlock()

	b.set("authUrl", start.AuthURL)
	b.set("statusText", "Spotify auth: waiting for browser callback...")

	_ = qt.QDesktopServices_OpenUrl(qt.NewQUrl3(start.AuthURL))

	result, err := start.Flow.Wait(flowCtx)
	if err != nil {
		b.finishWithError(err.Error())
		return
	}

	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		b.finishWithError(err.Error())
		return
	}

	persistCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := libspotdl.PersistInteractiveAuthResult(persistCtx, libspotdl.Config{
		Auth: libspotdl.AuthConfig{
			CredentialsFile: credentialsFile,
		},
	}, result); err != nil {
		b.finishWithError(err.Error())
		return
	}

	b.mu.Lock()
	if b.flowCancel != nil {
		b.flowCancel()
	}
	b.flow = nil
	b.flowCtx = nil
	b.flowCancel = nil
	b.mu.Unlock()

	b.set("isBusy", false)
	b.set("state", "completed")
	b.set("username", strings.TrimSpace(result.Username))
	b.set("lastError", "")
	if u := strings.TrimSpace(result.Username); u != "" {
		b.set("statusText", "Spotify auth: connected ("+u+")")
	} else {
		b.set("statusText", "Spotify auth: connected")
	}
	go b.loadLibrary()
}

func (b *SpotifyBridge) bootstrapExistingAuth() {
	b.set("isBusy", true)
	b.set("state", "checking")
	b.set("statusText", "Spotify auth: checking cached credentials...")

	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		b.set("isBusy", false)
		b.set("state", "error")
		b.set("lastError", err.Error())
		b.set("statusText", "Spotify auth: failed checking cache")
		return
	}

	hasStored, err := libspotdl.HasStoredCredentials(credentialsFile)
	if err != nil {
		b.set("isBusy", false)
		b.set("state", "error")
		b.set("lastError", err.Error())
		b.set("statusText", "Spotify auth: failed checking cache")
		return
	}

	if hasStored {
		b.set("isBusy", false)
		b.set("state", "completed")
		b.set("lastError", "")
		b.set("statusText", "Spotify auth: using cached credentials")
		go b.loadLibrary()
		return
	}

	b.set("isBusy", false)
	b.set("state", "idle")
	b.set("lastError", "")
	b.set("statusText", "Spotify auth: not connected")
}

func (b *SpotifyBridge) loadLibrary() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		b.set("libraryStatus", "Spotify library: failed ("+err.Error()+")")
		return
	}

	cfg := libspotdl.Config{
		Auth: libspotdl.AuthConfig{
			CredentialsFile: credentialsFile,
		},
	}

	snapshot, err := libspotdl.FetchLibrarySnapshot(ctx, cfg)
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "no cached spotify oauth access token") || strings.Contains(errText, "status 401") {
			b.set("state", "idle")
			b.set("lastError", err.Error())
			b.set("statusText", "Spotify auth: session expired, reauth needed")
			b.set("libraryStatus", "Spotify library: re-authenticate to refresh access token")
			go b.startFlow()
			return
		}
		if strings.Contains(errText, "insufficient client scope") {
			b.set("state", "idle")
			b.set("lastError", err.Error())
			b.set("statusText", "Spotify auth: additional permissions required, reauth needed")
			b.set("libraryStatus", "Spotify library: re-authenticate to grant playlist/library scopes")
			go b.startFlow()
			return
		}
		if libspotdl.IsSpotifyCredentialRefusedError(err) {
			b.set("state", "idle")
			b.set("lastError", err.Error())
			b.set("statusText", "Spotify auth: cached credentials rejected, reauth required")
			b.set("libraryStatus", "Spotify library: requires re-authentication")
			go b.startFlow()
			return
		}
		b.set("lastError", err.Error())
		if strings.Contains(errText, "status 429") || strings.Contains(errText, "rate limit") {
			b.set("libraryStatus", "Spotify library: rate limited by Spotify, please wait and retry")
			return
		}
		b.set("libraryStatus", "Spotify library: failed loading playlists ("+err.Error()+")")
		return
	}

	playlistsJSON := "[]"
	if encoded, encErr := json.Marshal(snapshot.Playlists); encErr == nil {
		playlistsJSON = string(encoded)
	}
	b.set("playlistsJson", playlistsJSON)
	b.set("playlistsCount", len(snapshot.Playlists))
	b.set("likedSongsName", snapshot.Liked.Name)
	b.set("likedSongsCount", snapshot.Liked.TrackCount)
	if snapshot.LikedAvailable {
		b.set("libraryStatus", fmt.Sprintf("Spotify library: loaded (%d playlists, %d liked songs)", len(snapshot.Playlists), snapshot.Liked.TrackCount))
		return
	}

	b.set("libraryStatus", fmt.Sprintf("Spotify library: playlists loaded (%d), liked songs unavailable", len(snapshot.Playlists)))
}

func (b *SpotifyBridge) openCollection(raw string) {
	parts := strings.SplitN(raw, "\n", 3)
	if len(parts) < 2 {
		return
	}

	contextURI := strings.TrimSpace(parts[0])
	title := strings.TrimSpace(parts[1])
	if contextURI == "" {
		return
	}
	if title == "" {
		title = "Tracks"
	}

	b.set("isLoadingTracks", true)
	b.set("trackListTitle", title)
	b.set("trackListStatus", "Loading tracks...")
	b.set("trackListJson", "[]")
	b.set("trackHasMore", false)
	b.set("trackTotal", 0)
	b.set("lastError", "")
	b.set("viewMode", "tracks")

	b.mu.Lock()
	b.trackContextURI = contextURI
	b.trackPageOffset = 0
	b.trackPageLimit = 80
	b.trackItems = nil
	b.loadingTracks = false
	b.mu.Unlock()

	b.loadNextTrackPage()
}

func (b *SpotifyBridge) loadMoreTracks() {
	b.loadNextTrackPage()
}

func (b *SpotifyBridge) loadNextTrackPage() {
	b.mu.Lock()
	contextURI := strings.TrimSpace(b.trackContextURI)
	offset := b.trackPageOffset
	limit := b.trackPageLimit
	if limit <= 0 {
		limit = 80
		b.trackPageLimit = limit
	}
	if contextURI == "" || b.loadingTracks {
		b.mu.Unlock()
		return
	}
	b.loadingTracks = true
	b.mu.Unlock()

	b.set("isLoadingTracks", true)
	if offset == 0 {
		b.set("trackListStatus", "Loading tracks...")
	} else {
		b.set("trackListStatus", "Loading more tracks...")
	}

	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		b.mu.Lock()
		b.loadingTracks = false
		b.mu.Unlock()
		b.set("isLoadingTracks", false)
		b.set("trackListStatus", "Failed loading tracks")
		b.set("lastError", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := libspotdl.Config{
		Auth: libspotdl.AuthConfig{CredentialsFile: credentialsFile},
	}

	page, err := libspotdl.FetchContextTrackSummariesPage(ctx, cfg, contextURI, offset, limit)
	if err != nil {
		b.mu.Lock()
		b.loadingTracks = false
		b.mu.Unlock()
		b.set("isLoadingTracks", false)
		b.set("trackListStatus", "Failed loading tracks")
		b.set("lastError", err.Error())
		return
	}

	b.mu.Lock()
	if contextURI != b.trackContextURI {
		b.loadingTracks = false
		b.mu.Unlock()
		b.set("isLoadingTracks", false)
		return
	}
	if offset == 0 {
		b.trackItems = make([]libspotdl.LibraryTrackSummary, 0, len(page.Items))
	}
	b.trackItems = append(b.trackItems, page.Items...)
	b.trackPageOffset = offset + len(page.Items)
	allTracks := append([]libspotdl.LibraryTrackSummary(nil), b.trackItems...)
	b.loadingTracks = false
	b.mu.Unlock()

	tracksJSON := "[]"
	if encoded, encErr := json.Marshal(allTracks); encErr == nil {
		tracksJSON = string(encoded)
	}

	b.set("trackListJson", tracksJSON)
	b.set("trackHasMore", page.HasMore)
	b.set("trackTotal", page.Total)
	loadedCount := len(allTracks)
	if page.HasMore {
		if page.Total > 0 {
			b.set("trackListStatus", fmt.Sprintf("Loaded %d of %d tracks", loadedCount, page.Total))
		} else {
			b.set("trackListStatus", fmt.Sprintf("Loaded %d tracks", loadedCount))
		}
	} else {
		if page.Total > 0 {
			b.set("trackListStatus", fmt.Sprintf("Loaded %d tracks", loadedCount))
		} else {
			b.set("trackListStatus", fmt.Sprintf("Loaded %d tracks", loadedCount))
		}
	}
	b.set("isLoadingTracks", false)
}

func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Music", "Voxora")
	}
	return ""
}

func (b *SpotifyBridge) downloadTrack(raw string) {
	parts := strings.SplitN(raw, "\n", 3)
	if len(parts) < 1 {
		return
	}

	trackURI := strings.TrimSpace(parts[0])
	trackName := "Track"
	if len(parts) >= 2 {
		if name := strings.TrimSpace(parts[1]); name != "" {
			trackName = name
		}
	}

	if !strings.HasPrefix(trackURI, "spotify:track:") {
		b.set("lastError", "invalid track uri")
		b.set("trackListStatus", "Failed starting download")
		return
	}

	b.set("isDownloadingTrack", true)
	b.set("lastError", "")
	b.set("trackListStatus", "Downloading \""+trackName+"\"...")

	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		b.set("isDownloadingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Download failed")
		return
	}

	outDir := defaultDownloadDir()
	if strings.TrimSpace(outDir) != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			b.set("isDownloadingTrack", false)
			b.set("lastError", err.Error())
			b.set("trackListStatus", "Download failed")
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	cfg := libspotdl.Config{
		Auth: libspotdl.AuthConfig{CredentialsFile: credentialsFile},
	}

	downloader, err := libspotdl.New(ctx, cfg)
	if err != nil {
		b.set("isDownloadingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Download failed")
		return
	}
	defer downloader.Close()

	results, err := downloader.Download(ctx, libspotdl.Request{
		Source:       trackURI,
		OutputDir:    outDir,
		OutputFormat: libspotdl.OutputFormatMP3,
		Overwrite:    true,
		Tagging: libspotdl.TaggingConfig{
			Enabled:        true,
			IncludeArtwork: true,
		},
	})
	if err != nil {
		b.set("isDownloadingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Download failed")
		return
	}

	b.set("isDownloadingTrack", false)
	if len(results) > 0 && strings.TrimSpace(results[0].OutputPath) != "" {
		b.set("trackListStatus", "Downloaded \""+trackName+"\" to "+results[0].OutputPath)
		return
	}
	b.set("trackListStatus", "Downloaded \""+trackName+"\"")
}

func (b *SpotifyBridge) finishWithError(message string) {
	b.mu.Lock()
	if b.flow != nil {
		b.flow.Cancel()
	}
	if b.flowCancel != nil {
		b.flowCancel()
	}
	b.flow = nil
	b.flowCtx = nil
	b.flowCancel = nil
	b.mu.Unlock()

	b.set("isBusy", false)
	b.set("state", "error")
	b.set("lastError", message)
	b.set("statusText", "Spotify auth: failed")
}

func (b *SpotifyBridge) set(key string, value any) {
	switch v := value.(type) {
	case bool:
		b.props.Insert(key, qt.NewQVariant8(v))
	case int:
		b.props.Insert(key, qt.NewQVariant4(v))
	case int64:
		b.props.Insert(key, qt.NewQVariant6(v))
	case float64:
		b.props.Insert(key, qt.NewQVariant9(v))
	case string:
		b.props.Insert(key, qt.NewQVariant14(v))
	default:
		b.props.Insert(key, qt.NewQVariant14(""))
	}
}
