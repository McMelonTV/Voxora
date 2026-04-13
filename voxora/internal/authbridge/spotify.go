package authbridge

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	libspotdl "github.com/McMelonTV/Voxora/libspotd"
	"github.com/dhowden/tag"
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

	downloadedByURI    map[string]string
	downloadedArtByURI map[string]string
	currentStream      string
	streamReadyNonce   int
	streamCancel       context.CancelFunc
	streamOpID         int
	prebufferCancel    context.CancelFunc
	prebufferTrackURI  string
	prebufferOpID      int

	transferMu           sync.Mutex
	sharedDownloader     *libspotdl.Downloader
	sharedCredsFile      string
	activeStreamFIFO     string
	activeStreamWrite    *os.File
	streamBufferedLatest int64

	streamHTTPListener net.Listener
	streamHTTPServer   *http.Server
	streamHTTPBaseURL  string
}

func NewSpotifyBridge() *SpotifyBridge {
	props := qml.NewQQmlPropertyMap()
	b := &SpotifyBridge{props: props, downloadedByURI: make(map[string]string), downloadedArtByURI: make(map[string]string)}
	b.startStreamHTTPServer()

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
	b.set("isStreamingTrack", false)
	b.set("streamTrackRequest", "")
	b.set("prebufferTrackRequest", "")
	b.set("streamPlayPath", "")
	b.set("streamPlayReadyNonce", 0)
	b.set("streamCacheReady", false)
	b.set("streamCachePath", "")
	b.set("streamCacheFilePath", "")
	b.set("streamBufferedBytes", int64(0))
	b.set("streamBufferedTotal", int64(0))
	b.set("clearStreamNonce", 0)
	b.set("clearStreamCacheAllNonce", 0)
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
		case "streamTrackRequest":
			go b.streamTrack(value.ToString())
		case "prebufferTrackRequest":
			go b.prebufferTrack(value.ToString())
		case "clearStreamNonce":
			go b.clearStreamCache()
		case "clearStreamCacheAllNonce":
			go b.clearAllStreamCache()
		case "navigateBackNonce":
			b.set("viewMode", "library")
			b.set("trackListStatus", "")
		}
	})

	b.loadDownloadedIndex()

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
	if b.streamCancel != nil {
		b.streamCancel()
		b.streamCancel = nil
	}
	if b.prebufferCancel != nil {
		b.prebufferCancel()
		b.prebufferCancel = nil
		b.prebufferTrackURI = ""
	}
	if strings.TrimSpace(b.activeStreamFIFO) != "" {
		_ = os.Remove(b.activeStreamFIFO)
		b.activeStreamFIFO = ""
	}
	if b.activeStreamWrite != nil {
		_ = b.activeStreamWrite.Close()
		b.activeStreamWrite = nil
	}
	if b.sharedDownloader != nil {
		_ = b.sharedDownloader.Close()
		b.sharedDownloader = nil
		b.sharedCredsFile = ""
	}
	if b.streamHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = b.streamHTTPServer.Shutdown(ctx)
		cancel()
		b.streamHTTPServer = nil
	}
	if b.streamHTTPListener != nil {
		_ = b.streamHTTPListener.Close()
		b.streamHTTPListener = nil
	}
	b.streamHTTPBaseURL = ""
}

func (b *SpotifyBridge) startStreamHTTPServer() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stream", b.handleStreamHTTP)
	mux.HandleFunc("/art", b.handleArtworkHTTP)

	server := &http.Server{Handler: mux}

	b.mu.Lock()
	b.streamHTTPListener = listener
	b.streamHTTPServer = server
	b.streamHTTPBaseURL = "http://" + listener.Addr().String()
	b.mu.Unlock()

	go func() {
		_ = server.Serve(listener)
	}()
}

func (b *SpotifyBridge) streamHTTPURL(kind, uri string) string {
	b.mu.Lock()
	base := b.streamHTTPBaseURL
	b.mu.Unlock()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	if strings.TrimSpace(uri) == "" {
		return ""
	}
	return fmt.Sprintf("%s/stream?kind=%s&uri=%s", base, kind, url.QueryEscape(uri))
}

func (b *SpotifyBridge) artworkHTTPURL(rawURL string) string {
	b.mu.Lock()
	base := b.streamHTTPBaseURL
	b.mu.Unlock()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	clean := strings.TrimSpace(rawURL)
	if clean == "" {
		return ""
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return fmt.Sprintf("%s/art?url=%s", base, url.QueryEscape(clean))
}

func (b *SpotifyBridge) embeddedArtworkHTTPURL(uri string) string {
	b.mu.Lock()
	base := b.streamHTTPBaseURL
	b.mu.Unlock()
	if strings.TrimSpace(base) == "" {
		return ""
	}
	cleanURI := strings.TrimSpace(uri)
	if cleanURI == "" {
		return ""
	}
	return fmt.Sprintf("%s/art?uri=%s", base, url.QueryEscape(cleanURI))
}

func (b *SpotifyBridge) handleStreamHTTP(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	uri := strings.TrimSpace(r.URL.Query().Get("uri"))
	if kind == "" || uri == "" {
		http.Error(w, "missing kind/uri", http.StatusBadRequest)
		return
	}

	var path string
	switch kind {
	case "play":
		path = streamPartPathForURI(uri)
	case "cache":
		path = streamCachePathForURI(uri)
	default:
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(path) == "" {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(path), time.Now(), file)
}

func (b *SpotifyBridge) handleArtworkHTTP(w http.ResponseWriter, r *http.Request) {
	if uri := strings.TrimSpace(r.URL.Query().Get("uri")); uri != "" {
		b.mu.Lock()
		embeddedPath := strings.TrimSpace(b.downloadedArtByURI[uri])
		b.mu.Unlock()
		if embeddedPath == "" {
			http.NotFound(w, r)
			return
		}
		b.serveArtworkFile(w, r, embeddedPath)
		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	cachePath := remoteArtworkCachePath(raw)
	if cachePath != "" {
		if _, statErr := os.Stat(cachePath); statErr == nil {
			b.serveArtworkFile(w, r, cachePath)
			return
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, raw, nil)
	if err != nil {
		http.Error(w, "request build failed", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Voxora/1.0")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "art fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		http.Error(w, "art fetch failed", http.StatusBadGateway)
		return
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if readErr != nil || len(body) == 0 {
		http.Error(w, "art fetch failed", http.StatusBadGateway)
		return
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}

	if cachePath != "" {
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
			tmp := cachePath + ".tmp"
			if writeErr := os.WriteFile(tmp, body, 0o644); writeErr == nil {
				_ = os.Rename(tmp, cachePath)
			}
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

func (b *SpotifyBridge) serveArtworkFile(w http.ResponseWriter, r *http.Request, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := file.Read(head)
	_, _ = file.Seek(0, io.SeekStart)
	contentType := http.DetectContentType(head[:n])
	if strings.TrimSpace(contentType) == "" {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, filepath.Base(path), time.Now(), file)
}

func artworkCacheDir() string {
	if runtime.GOOS == "android" {
		baseDir := filepath.Dir(bridgeCredentialsFile())
		if strings.TrimSpace(baseDir) != "" {
			return filepath.Join(baseDir, "artwork")
		}
	}

	cacheDir, err := os.UserCacheDir()
	if err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "voxora", "artwork")
	}
	return filepath.Join(os.TempDir(), "voxora", "artwork")
}

func remoteArtworkCachePath(rawURL string) string {
	clean := strings.TrimSpace(rawURL)
	if clean == "" {
		return ""
	}
	sum := sha1.Sum([]byte(clean))
	return filepath.Join(artworkCacheDir(), "remote", fmt.Sprintf("%x.img", sum))
}

func embeddedArtworkExt(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func embeddedArtworkPathForURI(uri, mimeType string) string {
	clean := sanitizeTrackID(uri)
	if clean == "" {
		return ""
	}
	return filepath.Join(artworkCacheDir(), "embedded", clean+embeddedArtworkExt(mimeType))
}

func (b *SpotifyBridge) startFlow() {
	log := newBridgeLogger().WithField("component", "spotify_auth")
	log.Info("starting interactive Spotify auth flow")

	b.mu.Lock()
	if b.flow != nil {
		b.mu.Unlock()
		log.Debug("auth flow already active; ignoring duplicate request")
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

	start, err := libspotdl.StartInteractiveAuthFlow(flowCtx, log, 0)
	if err != nil {
		log.WithError(err).Error("failed to start interactive auth flow")
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
	log.WithField("callback_port", start.CallbackPort).Info("interactive auth callback server started")
	log.WithField("auth_url", start.AuthURL).Debug("opening spotify auth URL in external browser")

	_ = qt.QDesktopServices_OpenUrl(qt.NewQUrl3(start.AuthURL))

	result, err := start.Flow.Wait(flowCtx)
	if err != nil {
		log.WithError(err).Error("interactive auth flow failed while waiting for callback")
		b.finishWithError(err.Error())
		return
	}
	log.WithField("username", strings.TrimSpace(result.Username)).Info("interactive auth callback completed")

	credentialsFile := bridgeCredentialsFile()

	persistCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := libspotdl.PersistInteractiveAuthResult(persistCtx, libspotdl.Config{
		Auth: libspotdl.AuthConfig{
			CredentialsFile: credentialsFile,
		},
		Logger: log,
	}, result); err != nil {
		log.WithError(err).Error("failed to persist spotify auth result")
		b.finishWithError(err.Error())
		return
	}
	log.WithField("credentials_file", credentialsFile).Info("spotify auth persisted successfully")

	b.mu.Lock()
	if b.flowCancel != nil {
		b.flowCancel()
	}
	b.flow = nil
	b.flowCtx = nil
	b.flowCancel = nil
	b.mu.Unlock()
	b.resetSharedDownloader()

	b.set("isBusy", false)
	b.set("state", "completed")
	b.set("username", strings.TrimSpace(result.Username))
	b.set("lastError", "")
	if u := strings.TrimSpace(result.Username); u != "" {
		b.set("statusText", "Spotify auth: connected ("+u+")")
	} else {
		b.set("statusText", "Spotify auth: connected")
	}
	log.Info("spotify auth flow completed")
	go b.loadLibrary()
}

func (b *SpotifyBridge) bootstrapExistingAuth() {
	b.set("isBusy", true)
	b.set("state", "checking")
	b.set("statusText", "Spotify auth: checking cached credentials...")

	credentialsFile := bridgeCredentialsFile()

	hasStored, err := libspotdl.HasStoredCredentials(credentialsFile)
	if err != nil {
		b.set("isBusy", false)
		b.set("state", "error")
		b.set("lastError", err.Error())
		b.set("statusText", "Spotify auth: failed checking cache")
		return
	}

	hasOAuth := false
	if token, tokenErr := libspotdl.CachedOAuthAccessToken(credentialsFile); tokenErr == nil && strings.TrimSpace(token) != "" {
		hasOAuth = true
	}

	if hasStored {
		b.set("isBusy", false)
		b.set("state", "completed")
		b.set("lastError", "")
		b.set("statusText", "Spotify auth: using cached credentials")
		go b.loadLibrary()
		return
	}

	if hasOAuth {
		b.set("isBusy", false)
		b.set("state", "idle")
		b.set("lastError", "")
		b.set("statusText", "Spotify auth: token cached, finish connection")
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

	downloader, err := b.sharedDownloaderFor(ctx)
	if err != nil {
		b.set("libraryStatus", "Spotify library: failed ("+err.Error()+")")
		return
	}

	snapshot, err := libspotdl.FetchLibrarySnapshotWithDownloader(ctx, downloader)
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	downloader, err := b.sharedDownloaderFor(ctx)
	if err != nil {
		b.mu.Lock()
		b.loadingTracks = false
		b.mu.Unlock()
		b.set("isLoadingTracks", false)
		b.set("trackListStatus", "Failed loading tracks")
		b.set("lastError", err.Error())
		return
	}

	page, err := libspotdl.FetchContextTrackSummariesPageWithDownloader(ctx, downloader, contextURI, offset, limit)
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

	allTracks = b.annotateDownloadedTracks(allTracks)
	b.mu.Lock()
	b.trackItems = append([]libspotdl.LibraryTrackSummary(nil), allTracks...)
	b.mu.Unlock()

	b.publishTrackList(allTracks)
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
	if runtime.GOOS == "android" {
		baseDir := filepath.Dir(bridgeCredentialsFile())
		if strings.TrimSpace(baseDir) != "" {
			return filepath.Join(baseDir, "downloads")
		}
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Music", "Voxora")
	}
	return ""
}

func streamFIFOPathForURI(uri string) string {
	return filepath.Join(streamCacheDir(), sanitizeTrackID(uri)+".live.fifo")
}

func (b *SpotifyBridge) resetSharedDownloader() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sharedDownloader != nil {
		_ = b.sharedDownloader.Close()
		b.sharedDownloader = nil
		b.sharedCredsFile = ""
	}
}

func (b *SpotifyBridge) sharedDownloaderFor(ctx context.Context) (*libspotdl.Downloader, error) {
	log := newBridgeLogger().WithField("component", "spotify_downloader")

	credentialsFile := bridgeCredentialsFile()

	b.mu.Lock()
	if b.sharedDownloader != nil && b.sharedCredsFile == credentialsFile {
		d := b.sharedDownloader
		b.mu.Unlock()
		return d, nil
	}
	old := b.sharedDownloader
	b.sharedDownloader = nil
	b.sharedCredsFile = ""
	b.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	ffmpegPath := bridgePrepareFFmpegEnv()
	d, err := libspotdl.New(ctx, libspotdl.Config{
		Auth:       libspotdl.AuthConfig{CredentialsFile: credentialsFile},
		Logger:     log,
		FFmpegPath: ffmpegPath,
	})
	log.WithField("ffmpeg_path", ffmpegPath).WithField("path", os.Getenv("PATH")).Debug("resolved ffmpeg path for downloader")
	if err != nil {
		log.WithError(err).Error("failed to create shared downloader")
		return nil, err
	}

	b.mu.Lock()
	if b.sharedDownloader != nil {
		existing := b.sharedDownloader
		b.mu.Unlock()
		_ = d.Close()
		return existing, nil
	}
	b.sharedDownloader = d
	b.sharedCredsFile = credentialsFile
	b.mu.Unlock()

	return d, nil
}

func streamCacheDir() string {
	if runtime.GOOS == "android" {
		baseDir := filepath.Dir(bridgeCredentialsFile())
		if strings.TrimSpace(baseDir) != "" {
			return filepath.Join(baseDir, "stream")
		}
	}

	cacheDir, err := os.UserCacheDir()
	if err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "voxora", "stream")
	}
	return filepath.Join(os.TempDir(), "voxora", "stream")
}

const (
	streamCacheReadyBytes           = int64(32 * 1024)
	streamPlaybackReadyBytes        = int64(512 * 1024)
	streamPlaybackReadyBytesAndroid = int64(2 * 1024 * 1024)
	streamCacheMinFree              = uint64(1024 * 1024 * 1024) // 1 GiB
)

func streamPlaybackReadyThreshold() int64 {
	if runtime.GOOS == "android" {
		return streamPlaybackReadyBytesAndroid
	}
	return streamPlaybackReadyBytes
}

type cachedStreamFile struct {
	path    string
	modTime time.Time
	size    int64
}

func streamCachePathForURI(uri string) string {
	return filepath.Join(streamCacheDir(), sanitizeTrackID(uri)+".stream.ogg")
}

func streamPartPathForURI(uri string) string {
	return filepath.Join(streamCacheDir(), sanitizeTrackID(uri)+".stream.play.ogg")
}

func streamCacheDoneMarkerPathForStream(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return path + ".done"
}

func isUsableCachedStream(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if info.Size() < streamCacheReadyBytes {
		return false
	}
	done := streamCacheDoneMarkerPathForStream(path)
	if done == "" {
		return false
	}
	if marker, err := os.Stat(done); err != nil || marker.IsDir() {
		return false
	}
	return true
}

func freeDiskBytes(path string) uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}

func listStreamCacheFiles(dir string) []cachedStreamFile {
	out := make([]cachedStreamFile, 0, 64)
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".stream.ogg") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		out = append(out, cachedStreamFile{path: path, modTime: info.ModTime(), size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].modTime.Before(out[j].modTime) })
	return out
}

func (b *SpotifyBridge) pruneStreamCacheIfLowDisk(cacheDir string) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir == "" {
		return
	}
	free := freeDiskBytes(cacheDir)
	if free >= streamCacheMinFree {
		return
	}

	b.mu.Lock()
	active := strings.TrimSpace(b.currentStream)
	b.mu.Unlock()

	files := listStreamCacheFiles(cacheDir)
	for _, f := range files {
		if free >= streamCacheMinFree {
			break
		}
		if strings.TrimSpace(f.path) == "" || f.path == active {
			continue
		}
		if err := os.Remove(f.path); err != nil {
			continue
		}
		free = freeDiskBytes(cacheDir)
	}
}

func sanitizeTrackID(uri string) string {
	id := strings.TrimPrefix(strings.TrimSpace(uri), "spotify:track:")
	id = strings.TrimSpace(id)
	if id == "" {
		return "track"
	}
	return id
}

func (b *SpotifyBridge) clearStreamCache() {
	b.mu.Lock()
	b.streamOpID++
	if b.streamCancel != nil {
		b.streamCancel()
		b.streamCancel = nil
	}
	if b.activeStreamWrite != nil {
		_ = b.activeStreamWrite.Close()
		b.activeStreamWrite = nil
	}
	fifoPath := strings.TrimSpace(b.activeStreamFIFO)
	b.activeStreamFIFO = ""
	b.currentStream = ""
	b.mu.Unlock()

	if fifoPath != "" {
		_ = os.Remove(fifoPath)
	}

	b.set("streamPlayPath", "")
	b.set("streamCacheReady", false)
	b.set("streamCachePath", "")
	b.set("streamCacheFilePath", "")
	b.mu.Lock()
	b.streamBufferedLatest = 0
	b.mu.Unlock()
	b.set("streamBufferedBytes", int64(0))
	b.set("streamBufferedTotal", int64(0))
}

func (b *SpotifyBridge) clearAllStreamCache() {
	b.clearStreamCache()

	cacheDir := strings.TrimSpace(streamCacheDir())
	if cacheDir == "" {
		b.set("trackListStatus", "Stream cache cleared")
		return
	}

	removed := 0
	_ = filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".stream.ogg") || strings.HasSuffix(name, ".stream.play.ogg") || strings.HasSuffix(name, ".stream.ogg.part") || strings.HasSuffix(name, ".stream.ogg.done") || strings.HasSuffix(name, ".live.fifo") || strings.HasSuffix(name, ".prefetch.part") {
			if remErr := os.Remove(path); remErr == nil {
				removed++
			}
		}
		return nil
	})

	b.set("trackListStatus", fmt.Sprintf("Cleared %d stream cache file(s)", removed))
}

func (b *SpotifyBridge) prebufferTrack(raw string) {
	parts := strings.SplitN(raw, "\n", 3)
	if len(parts) < 1 {
		return
	}

	trackURI := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(trackURI, "spotify:track:") {
		return
	}

	outputPath := streamCachePathForURI(trackURI)
	if isUsableCachedStream(outputPath) {
		_ = os.Chtimes(outputPath, time.Now(), time.Now())
		return
	}

	cacheDir := streamCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	b.pruneStreamCacheIfLowDisk(cacheDir)

	b.mu.Lock()
	if b.prebufferTrackURI == trackURI && b.prebufferCancel != nil {
		b.mu.Unlock()
		return
	}
	if b.prebufferCancel != nil {
		b.prebufferCancel()
		b.prebufferCancel = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	b.prebufferCancel = cancel
	b.prebufferTrackURI = trackURI
	b.prebufferOpID++
	opID := b.prebufferOpID
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		if b.prebufferOpID == opID {
			b.prebufferCancel = nil
			b.prebufferTrackURI = ""
		}
		b.mu.Unlock()
	}()

	credentialsFile := bridgeCredentialsFile()
	ffmpegPath := bridgePrepareFFmpegEnv()
	log := newBridgeLogger().WithField("component", "spotify_prebuffer")
	loader, err := libspotdl.New(ctx, libspotdl.Config{
		Auth:       libspotdl.AuthConfig{CredentialsFile: credentialsFile},
		Logger:     log,
		FFmpegPath: ffmpegPath,
	})
	if err != nil {
		cancel()
		return
	}
	defer func() {
		_ = loader.Close()
	}()

	tmpPath := outputPath + ".prefetch.part"
	_ = os.Remove(tmpPath)
	_ = os.Remove(streamCacheDoneMarkerPathForStream(outputPath))

	f, err := os.Create(tmpPath)
	if err != nil {
		cancel()
		return
	}
	defer func() {
		_ = f.Close()
	}()

	_, _, err = loader.StreamTrack(ctx, trackURI, []io.Writer{f}, nil)
	cancel()
	if err != nil {
		_ = os.Remove(tmpPath)
		return
	}

	if err := f.Sync(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}

	b.mu.Lock()
	isCurrent := b.prebufferOpID == opID
	b.mu.Unlock()
	if !isCurrent {
		_ = os.Remove(tmpPath)
		return
	}

	if err := os.Rename(tmpPath, outputPath); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if doneMarkerPath := streamCacheDoneMarkerPathForStream(outputPath); doneMarkerPath != "" {
		_ = os.WriteFile(doneMarkerPath, []byte("ok\n"), 0o644)
	}
}

func (b *SpotifyBridge) streamTrack(raw string) {
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
		b.set("trackListStatus", "Failed starting stream")
		return
	}

	b.clearStreamCache()
	b.mu.Lock()
	b.streamOpID++
	opID := b.streamOpID
	b.mu.Unlock()

	b.set("isStreamingTrack", true)
	b.set("lastError", "")
	b.set("trackListStatus", "Buffering \""+trackName+"\"...")
	b.set("streamCacheReady", false)
	b.set("streamCachePath", "")
	b.mu.Lock()
	b.streamBufferedLatest = 0
	b.mu.Unlock()
	b.set("streamBufferedBytes", int64(0))
	b.set("streamBufferedTotal", int64(0))

	cacheDir := streamCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		b.set("isStreamingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}
	b.pruneStreamCacheIfLowDisk(cacheDir)

	outputPath := streamCachePathForURI(trackURI)
	partPath := streamPartPathForURI(trackURI)
	activeOutputPath := partPath
	doneMarkerPath := streamCacheDoneMarkerPathForStream(outputPath)
	playURL := b.streamHTTPURL("play", trackURI)
	cacheURL := b.streamHTTPURL("cache", trackURI)
	if strings.TrimSpace(playURL) == "" {
		playURL = activeOutputPath
	}
	if strings.TrimSpace(cacheURL) == "" {
		cacheURL = outputPath
	}
	if isUsableCachedStream(outputPath) {
		_ = os.Chtimes(outputPath, time.Now(), time.Now())
		if info, statErr := os.Stat(outputPath); statErr == nil {
			b.mu.Lock()
			b.streamBufferedLatest = info.Size()
			b.mu.Unlock()
			b.set("streamBufferedBytes", info.Size())
			b.set("streamBufferedTotal", info.Size())
		}
		b.mu.Lock()
		if b.streamOpID == opID {
			b.currentStream = outputPath
			b.streamReadyNonce++
			nonce := b.streamReadyNonce
			b.mu.Unlock()
			b.set("streamPlayPath", outputPath)
			b.set("streamPlayReadyNonce", nonce)
			b.set("streamCacheReady", true)
			b.set("streamCachePath", outputPath)
			b.set("streamCacheFilePath", outputPath)
			b.set("isStreamingTrack", false)
			b.set("trackListStatus", "Playing cached stream \""+trackName+"\"")
			return
		}
		b.mu.Unlock()
		b.set("isStreamingTrack", false)
		return
	}
	if doneMarkerPath != "" {
		_ = os.Remove(doneMarkerPath)
	}
	_ = os.Remove(activeOutputPath)

	useFIFO := false
	fifoPath := ""
	if useFIFO {
		fifoPath = streamFIFOPathForURI(trackURI)
		_ = os.Remove(fifoPath)
		if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
			b.set("isStreamingTrack", false)
			b.set("lastError", err.Error())
			b.set("trackListStatus", "Stream failed")
			return
		}

		b.mu.Lock()
		b.activeStreamFIFO = fifoPath
		if b.streamOpID == opID {
			b.streamReadyNonce++
			nonce := b.streamReadyNonce
			b.mu.Unlock()
			b.set("streamPlayPath", fifoPath)
			b.set("streamPlayReadyNonce", nonce)
		} else {
			b.mu.Unlock()
			_ = os.Remove(fifoPath)
			b.set("isStreamingTrack", false)
			return
		}
	} else {
		b.mu.Lock()
		if b.streamOpID != opID {
			b.mu.Unlock()
			b.set("isStreamingTrack", false)
			return
		}
		b.mu.Unlock()
	}

	cacheFile, err := os.Create(activeOutputPath)
	if err != nil {
		if useFIFO {
			_ = os.Remove(fifoPath)
		}
		b.set("isStreamingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}

	var cacheMirror *os.File
	streamWriter := io.Writer(cacheFile)
	if activeOutputPath != outputPath {
		cacheMirror, err = os.Create(outputPath)
		if err != nil {
			_ = cacheFile.Close()
			if useFIFO {
				_ = os.Remove(fifoPath)
			}
			b.set("isStreamingTrack", false)
			b.set("lastError", err.Error())
			b.set("trackListStatus", "Stream failed")
			return
		}
		streamWriter = io.MultiWriter(cacheFile, cacheMirror)
	}

	cacheFileClosed := false
	closeCacheFile := func() {
		if cacheFileClosed {
			return
		}
		_ = cacheFile.Sync()
		_ = cacheFile.Close()
		if cacheMirror != nil {
			_ = cacheMirror.Sync()
			_ = cacheMirror.Close()
		}
		cacheFileClosed = true
	}
	defer closeCacheFile()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)

	b.mu.Lock()
	b.streamCancel = cancel
	b.currentStream = activeOutputPath
	b.mu.Unlock()

	downloadDone := make(chan struct{})
	tailDone := make(chan struct{})
	if useFIFO {
		go func() {
			defer close(tailDone)

			fifoWriter, ferr := os.OpenFile(fifoPath, os.O_WRONLY, 0)
			if ferr != nil {
				return
			}
			defer func() {
				_ = fifoWriter.Close()
				b.mu.Lock()
				if b.activeStreamWrite == fifoWriter {
					b.activeStreamWrite = nil
				}
				b.mu.Unlock()
			}()

			b.mu.Lock()
			if b.streamOpID == opID {
				b.activeStreamWrite = fifoWriter
			}
			b.mu.Unlock()

			cacheReader, rerr := os.Open(activeOutputPath)
			if rerr != nil {
				return
			}
			defer cacheReader.Close()

			buf := make([]byte, 64*1024)
			downloadFinished := false
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				n, readErr := cacheReader.Read(buf)
				if n > 0 {
					written := 0
					for written < n {
						wn, werr := fifoWriter.Write(buf[written:n])
						if werr != nil {
							if errors.Is(werr, syscall.EPIPE) {
								select {
								case <-ctx.Done():
									return
								case <-time.After(15 * time.Millisecond):
								}
								continue
							}
							select {
							case <-ctx.Done():
								return
							case <-time.After(15 * time.Millisecond):
							}
							continue
						}
						if wn <= 0 {
							select {
							case <-ctx.Done():
								return
							case <-time.After(15 * time.Millisecond):
							}
							continue
						}
						written += wn
					}
				}

				if readErr == nil {
					continue
				}
				if !errors.Is(readErr, io.EOF) {
					select {
					case <-ctx.Done():
						return
					case <-time.After(15 * time.Millisecond):
					}
					continue
				}

				if !downloadFinished {
					select {
					case <-downloadDone:
						downloadFinished = true
					default:
					}
				}

				if downloadFinished {
					pos, _ := cacheReader.Seek(0, io.SeekCurrent)
					if info, serr := os.Stat(activeOutputPath); serr == nil && pos >= info.Size() {
						return
					}
				}

				select {
				case <-ctx.Done():
					return
				case <-time.After(15 * time.Millisecond):
				}
			}
		}()
	} else {
		close(tailDone)
	}

	downloader, err := b.sharedDownloaderFor(ctx)
	if err != nil {
		cancel()
		close(downloadDone)
		<-tailDone
		if useFIFO {
			_ = os.Remove(fifoPath)
		}
		b.set("isStreamingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}

	b.transferMu.Lock()
	defer b.transferMu.Unlock()

	readySignaled := false
	readyThreshold := streamPlaybackReadyThreshold()
	_, _, err = downloader.StreamTrack(ctx, trackURI, []io.Writer{streamWriter}, func(p libspotdl.Progress) {
		b.mu.Lock()
		isCurrent := b.streamOpID == opID
		b.mu.Unlock()
		if !isCurrent {
			return
		}

		if p.Stage == "downloading" {
			b.mu.Lock()
			if p.BytesWritten > b.streamBufferedLatest {
				b.streamBufferedLatest = p.BytesWritten
			}
			latest := b.streamBufferedLatest
			b.mu.Unlock()
			b.set("streamBufferedBytes", latest)
			if p.TotalBytes > 0 {
				b.set("streamBufferedTotal", p.TotalBytes)
			}
		}

		if !readySignaled && p.Stage == "downloading" && p.BytesWritten >= readyThreshold {
			readySignaled = true
			if !useFIFO {
				b.mu.Lock()
				if b.streamOpID == opID {
					b.streamReadyNonce++
					nonce := b.streamReadyNonce
					b.mu.Unlock()
					b.set("streamPlayPath", playURL)
					b.set("streamPlayReadyNonce", nonce)
				} else {
					b.mu.Unlock()
				}
			}
			b.set("streamCacheReady", true)
			b.set("streamCachePath", outputPath)
			b.set("streamCacheFilePath", outputPath)
			b.set("isStreamingTrack", false)
			b.set("trackListStatus", "Playing streamed \""+trackName+"\"")
		}
	})
	close(downloadDone)
	<-tailDone
	cancel()
	if useFIFO {
		_ = os.Remove(fifoPath)
	}
	b.mu.Lock()
	if b.activeStreamFIFO == fifoPath {
		b.activeStreamFIFO = ""
	}
	b.mu.Unlock()
	b.mu.Lock()
	isCurrent := b.streamOpID == opID
	b.mu.Unlock()
	if !isCurrent {
		b.set("isStreamingTrack", false)
		return
	}
	if err != nil {
		b.set("isStreamingTrack", false)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			b.set("trackListStatus", "Stream stopped")
			return
		}
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}

	closeCacheFile()
	if doneMarkerPath != "" {
		_ = os.WriteFile(doneMarkerPath, []byte("ok\n"), 0o644)
	}
	if !useFIFO && activeOutputPath != outputPath {
		b.mu.Lock()
		if b.streamOpID == opID {
			b.currentStream = activeOutputPath
		}
		b.mu.Unlock()
	}

	if !readySignaled {
		if !useFIFO {
			b.mu.Lock()
			if b.streamOpID == opID {
				b.currentStream = activeOutputPath
				b.mu.Unlock()
			} else {
				b.mu.Unlock()
			}
		}
		b.set("streamCacheReady", true)
		b.set("streamCachePath", outputPath)
		b.set("streamCacheFilePath", outputPath)
	}
	if info, statErr := os.Stat(outputPath); statErr == nil {
		b.mu.Lock()
		b.streamBufferedLatest = info.Size()
		b.mu.Unlock()
		b.set("streamBufferedBytes", info.Size())
		b.set("streamBufferedTotal", info.Size())
	}
	b.set("streamCacheReady", true)
	b.set("streamCachePath", outputPath)
	b.set("streamCacheFilePath", outputPath)

	b.mu.Lock()
	if b.streamOpID == opID {
		b.streamCancel = nil
	}
	b.mu.Unlock()
	b.set("isStreamingTrack", false)
	b.set("trackListStatus", "Playing streamed \""+trackName+"\"")
}

var trackURIRegex = regexp.MustCompile(`spotify:track:[A-Za-z0-9]+`)

func extractTrackURIFromMediaTags(filePath string) string {
	log := newBridgeLogger().WithField("component", "download_scan").WithField("file", filePath)

	file, openErr := os.Open(filePath)
	if openErr == nil {
		meta, metaErr := tag.ReadFrom(file)
		_ = file.Close()
		if metaErr == nil {
			fields := map[string]string{}
			if v := strings.TrimSpace(meta.Title()); v != "" {
				fields["title"] = v
			}
			if v := strings.TrimSpace(meta.Artist()); v != "" {
				fields["artist"] = v
			}
			if v := strings.TrimSpace(meta.Album()); v != "" {
				fields["album"] = v
			}
			if v := strings.TrimSpace(meta.Genre()); v != "" {
				fields["genre"] = v
			}
			if c := meta.Raw(); c != nil {
				for k, rawV := range c {
					key := strings.ToLower(strings.TrimSpace(k))
					if key == "" {
						continue
					}
					value := strings.TrimSpace(fmt.Sprintf("%v", rawV))
					if value == "" {
						continue
					}
					fields[key] = value
				}
			}

			if len(fields) > 0 {
				log.WithField("tags", fields).Debug("metadata tags read from file")
			} else {
				log.Debug("metadata parser found no tags")
			}

			for _, v := range fields {
				if match := strings.TrimSpace(trackURIRegex.FindString(v)); match != "" {
					log.WithField("track_uri", match).Debug("extracted spotify URI from native metadata tags")
					return match
				}
			}
			log.Debug("native metadata tags found but no spotify URI present")
		} else {
			log.WithError(metaErr).Debug("native metadata parser failed")
		}
	} else {
		log.WithError(openErr).Debug("failed opening file for native metadata parser")
	}

	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format_tags=comment,description",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	out, err := cmd.Output()
	if err == nil {
		match := trackURIRegex.FindString(string(out))
		if strings.TrimSpace(match) == "" {
			log.Debug("ffprobe succeeded but no spotify URI found in tags")
		} else {
			log.WithField("track_uri", strings.TrimSpace(match)).Debug("extracted spotify URI using ffprobe")
		}
		return strings.TrimSpace(match)
	}
	log.WithError(err).Debug("ffprobe failed, falling back to ffmpeg metadata")

	ffmpegBin := strings.TrimSpace(os.Getenv("VOXORA_FFMPEG_PATH"))
	if ffmpegBin == "" {
		ffmpegBin = bridgePrepareFFmpegEnv()
	}
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	// Android often lacks ffprobe in PATH. Fallback to ffmpeg ffmetadata output,
	// which still includes embedded comment/description tags with spotify:track URI.
	metaCmd := exec.Command(
		ffmpegBin,
		"-v", "error",
		"-i", filePath,
		"-f", "ffmetadata",
		"-",
	)
	metaOut, metaErr := metaCmd.Output()
	if metaErr != nil {
		log.WithError(metaErr).WithField("ffmpeg_bin", ffmpegBin).Debug("ffmpeg metadata fallback failed")
		return ""
	}
	match := trackURIRegex.FindString(string(metaOut))
	if strings.TrimSpace(match) == "" {
		log.WithField("ffmpeg_bin", ffmpegBin).Debug("ffmpeg metadata fallback found no spotify URI")
	} else {
		log.WithField("ffmpeg_bin", ffmpegBin).WithField("track_uri", strings.TrimSpace(match)).Debug("extracted spotify URI using ffmpeg metadata fallback")
	}
	return strings.TrimSpace(match)
}

func extractEmbeddedArtworkFromMedia(filePath string) ([]byte, string) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, ""
	}
	defer file.Close()

	meta, err := tag.ReadFrom(file)
	if err != nil {
		return nil, ""
	}
	pic := meta.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil, ""
	}
	return pic.Data, strings.TrimSpace(pic.MIMEType)
}

func cacheEmbeddedArtworkForTrackURI(uri, mediaPath string) string {
	cleanURI := strings.TrimSpace(uri)
	cleanPath := strings.TrimSpace(mediaPath)
	if cleanURI == "" || cleanPath == "" {
		return ""
	}

	data, mimeType := extractEmbeddedArtworkFromMedia(cleanPath)
	if len(data) == 0 {
		return ""
	}

	artPath := embeddedArtworkPathForURI(cleanURI, mimeType)
	if artPath == "" {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(artPath), 0o755); err != nil {
		return ""
	}
	tmp := artPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return ""
	}
	if err := os.Rename(tmp, artPath); err != nil {
		return ""
	}
	return artPath
}

func isScannableAudioFile(path string) bool {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(path)))
	switch ext {
	case ".mp3", ".flac", ".ogg", ".m4a", ".aac", ".wav", ".opus":
		return true
	default:
		return false
	}
}

func scanDownloadedAudioTags(downloadDir string) map[string]string {
	log := newBridgeLogger().WithField("component", "download_scan").WithField("download_dir", downloadDir)
	result := make(map[string]string)
	dir := strings.TrimSpace(downloadDir)
	if dir == "" {
		log.Debug("download scan skipped: empty directory")
		return result
	}

	inspected := 0
	matches := 0

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.WithError(err).WithField("path", path).Debug("download scan walk error")
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isScannableAudioFile(path) {
			return nil
		}
		inspected++

		uri := extractTrackURIFromMediaTags(path)
		if uri == "" {
			log.WithField("file", path).Debug("audio file has no spotify URI tag")
			return nil
		}
		result[uri] = path
		matches++
		log.WithField("file", path).WithField("track_uri", uri).Debug("recognized downloaded track from metadata")
		return nil
	})

	log.WithField("inspected_files", inspected).WithField("matched_tracks", matches).Debug("completed downloaded tracks scan")

	return result
}

func (b *SpotifyBridge) loadDownloadedIndex() {
	log := newBridgeLogger().WithField("component", "download_scan")
	clean := make(map[string]string)
	art := make(map[string]string)
	for uri, filePath := range scanDownloadedAudioTags(defaultDownloadDir()) {
		clean[uri] = filePath
		if artPath := cacheEmbeddedArtworkForTrackURI(uri, filePath); artPath != "" {
			art[uri] = artPath
		}
	}

	b.mu.Lock()
	b.downloadedByURI = clean
	b.downloadedArtByURI = art
	b.mu.Unlock()

	log.WithField("recognized_tracks", len(clean)).WithField("embedded_art_tracks", len(art)).Debug("loaded downloaded tracks from metadata scan")
}

func (b *SpotifyBridge) annotateDownloadedTracks(items []libspotdl.LibraryTrackSummary) []libspotdl.LibraryTrackSummary {
	b.mu.Lock()
	index := make(map[string]string, len(b.downloadedByURI))
	for uri, filePath := range b.downloadedByURI {
		index[uri] = filePath
	}
	b.mu.Unlock()

	out := make([]libspotdl.LibraryTrackSummary, 0, len(items))
	missing := make([]string, 0)
	for _, item := range items {
		marked := item
		if p, ok := index[item.URI]; ok {
			if _, err := os.Stat(p); err == nil {
				marked.Downloaded = true
				marked.DownloadedPath = p
			} else {
				marked.Downloaded = false
				marked.DownloadedPath = ""
				missing = append(missing, item.URI)
			}
		}
		out = append(out, marked)
	}

	if len(missing) > 0 {
		b.mu.Lock()
		for _, uri := range missing {
			delete(b.downloadedByURI, uri)
			delete(b.downloadedArtByURI, uri)
		}
		b.mu.Unlock()
	}
	return out
}

func (b *SpotifyBridge) publishTrackList(items []libspotdl.LibraryTrackSummary) {
	b.mu.Lock()
	artIndex := make(map[string]string, len(b.downloadedArtByURI))
	for uri, artPath := range b.downloadedArtByURI {
		artIndex[uri] = artPath
	}
	b.mu.Unlock()

	if len(items) > 0 {
		normalized := make([]libspotdl.LibraryTrackSummary, 0, len(items))
		for _, item := range items {
			if item.Downloaded {
				if artPath, ok := artIndex[item.URI]; ok {
					if _, statErr := os.Stat(artPath); statErr == nil {
						if localURL := b.embeddedArtworkHTTPURL(item.URI); localURL != "" {
							item.AlbumArtURL = localURL
							normalized = append(normalized, item)
							continue
						}
					}
				}
			}
			if proxied := b.artworkHTTPURL(item.AlbumArtURL); proxied != "" {
				item.AlbumArtURL = proxied
			}
			normalized = append(normalized, item)
		}
		items = normalized
	}

	tracksJSON := "[]"
	if encoded, encErr := json.Marshal(items); encErr == nil {
		tracksJSON = string(encoded)
	}
	b.set("trackListJson", tracksJSON)
}

func (b *SpotifyBridge) downloadTrack(raw string) {
	log := newBridgeLogger().WithField("component", "spotify_downloader")

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

	downloader, err := b.sharedDownloaderFor(ctx)
	if err != nil {
		b.set("isDownloadingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Download failed")
		return
	}

	b.transferMu.Lock()
	defer b.transferMu.Unlock()

	primaryReq := libspotdl.Request{
		Source:       trackURI,
		OutputDir:    outDir,
		OutputFormat: libspotdl.OutputFormatMP3,
		Overwrite:    true,
		Tagging: libspotdl.TaggingConfig{
			Enabled:        true,
			IncludeArtwork: true,
		},
	}

	results, err := downloader.Download(ctx, primaryReq)
	if err != nil {
		errText := strings.ToLower(err.Error())
		status := "Download failed"
		switch {
		case strings.Contains(errText, "ffmpeg failed"):
			status = "Download failed: ffmpeg post-process"
		case strings.Contains(errText, "failed downloading artwork") || strings.Contains(errText, "download artwork") || strings.Contains(errText, "artwork"):
			status = "Download failed: artwork stage"
		case strings.Contains(errText, "open raw stream") || strings.Contains(errText, "read spotify stream") || strings.Contains(errText, "chunk"):
			status = "Download failed: stream read"
		case strings.Contains(errText, "write output") || strings.Contains(errText, "create temporary source file") || strings.Contains(errText, "move output"):
			status = "Download failed: file write"
		}

		log.WithError(err).WithField("track_uri", trackURI).WithField("stage", status).Error("track download failed")
		b.set("isDownloadingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", status)
		return
	}

	b.set("isDownloadingTrack", false)
	if len(results) > 0 && strings.TrimSpace(results[0].OutputPath) != "" {
		downloadedPath := strings.TrimSpace(results[0].OutputPath)
		embeddedArtPath := cacheEmbeddedArtworkForTrackURI(trackURI, downloadedPath)
		b.mu.Lock()
		if b.downloadedByURI == nil {
			b.downloadedByURI = make(map[string]string)
		}
		if b.downloadedArtByURI == nil {
			b.downloadedArtByURI = make(map[string]string)
		}
		b.downloadedByURI[trackURI] = downloadedPath
		if embeddedArtPath != "" {
			b.downloadedArtByURI[trackURI] = embeddedArtPath
		}
		for i := range b.trackItems {
			if b.trackItems[i].URI == trackURI {
				b.trackItems[i].Downloaded = true
				b.trackItems[i].DownloadedPath = downloadedPath
				break
			}
		}
		updatedTracks := append([]libspotdl.LibraryTrackSummary(nil), b.trackItems...)
		b.mu.Unlock()
		b.publishTrackList(updatedTracks)
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
