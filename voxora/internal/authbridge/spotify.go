package authbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
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

	downloadedByURI  map[string]string
	currentStream    string
	streamReadyNonce int
	streamCancel     context.CancelFunc
	streamOpID       int

	transferMu           sync.Mutex
	sharedDownloader     *libspotdl.Downloader
	sharedCredsFile      string
	activeStreamFIFO     string
	activeStreamWrite    *os.File
	streamBufferedLatest int64
}

type downloadedTrackIndex struct {
	Tracks map[string]string `json:"tracks"`
}

func NewSpotifyBridge() *SpotifyBridge {
	props := qml.NewQQmlPropertyMap()
	b := &SpotifyBridge{props: props, downloadedByURI: make(map[string]string)}

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
	b.set("streamPlayPath", "")
	b.set("streamPlayReadyNonce", 0)
	b.set("streamCacheReady", false)
	b.set("streamCachePath", "")
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
	credentialsFile, err := libspotdl.ResolveCredentialsFile("")
	if err != nil {
		return nil, err
	}

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

	d, err := libspotdl.New(ctx, libspotdl.Config{Auth: libspotdl.AuthConfig{CredentialsFile: credentialsFile}})
	if err != nil {
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
	cacheDir, err := os.UserCacheDir()
	if err == nil && strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "voxora", "stream")
	}
	return filepath.Join(os.TempDir(), "voxora", "stream")
}

const (
	streamCacheReadyBytes = int64(32 * 1024)
	streamCacheMinFree    = uint64(1024 * 1024 * 1024) // 1 GiB
)

type cachedStreamFile struct {
	path    string
	modTime time.Time
	size    int64
}

func streamCachePathForURI(uri string) string {
	return filepath.Join(streamCacheDir(), sanitizeTrackID(uri)+".stream.ogg")
}

func isUsableCachedStream(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Size() >= streamCacheReadyBytes
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
		if strings.HasSuffix(name, ".stream.ogg") || strings.HasSuffix(name, ".live.fifo") {
			if remErr := os.Remove(path); remErr == nil {
				removed++
			}
		}
		return nil
	})

	b.set("trackListStatus", fmt.Sprintf("Cleared %d stream cache file(s)", removed))
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
			b.set("isStreamingTrack", false)
			b.set("trackListStatus", "Playing cached stream \""+trackName+"\"")
			return
		}
		b.mu.Unlock()
		b.set("isStreamingTrack", false)
		return
	}

	fifoPath := streamFIFOPathForURI(trackURI)
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

	cacheFile, err := os.Create(outputPath)
	if err != nil {
		_ = os.Remove(fifoPath)
		b.set("isStreamingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}
	defer cacheFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)

	b.mu.Lock()
	b.streamCancel = cancel
	b.currentStream = outputPath
	b.mu.Unlock()

	downloadDone := make(chan struct{})
	tailDone := make(chan struct{})
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

		cacheReader, rerr := os.Open(outputPath)
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
				if info, serr := os.Stat(outputPath); serr == nil && pos >= info.Size() {
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

	downloader, err := b.sharedDownloaderFor(ctx)
	if err != nil {
		cancel()
		close(downloadDone)
		<-tailDone
		_ = os.Remove(fifoPath)
		b.set("isStreamingTrack", false)
		b.set("lastError", err.Error())
		b.set("trackListStatus", "Stream failed")
		return
	}

	b.transferMu.Lock()
	defer b.transferMu.Unlock()

	readySignaled := false
	_, _, err = downloader.StreamTrack(ctx, trackURI, []io.Writer{cacheFile}, func(p libspotdl.Progress) {
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

		if !readySignaled && p.Stage == "downloading" && p.BytesWritten >= streamCacheReadyBytes {
			readySignaled = true
			b.set("streamCacheReady", true)
			b.set("streamCachePath", outputPath)
			b.set("isStreamingTrack", false)
			b.set("trackListStatus", "Playing streamed \""+trackName+"\"")
		}
	})
	close(downloadDone)
	<-tailDone
	cancel()
	_ = os.Remove(fifoPath)
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

	if !readySignaled {
		b.set("streamCacheReady", true)
		b.set("streamCachePath", outputPath)
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

	b.mu.Lock()
	if b.streamOpID == opID {
		b.streamCancel = nil
	}
	b.mu.Unlock()
	b.set("isStreamingTrack", false)
	b.set("trackListStatus", "Playing streamed \""+trackName+"\"")
}

func downloadIndexPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, "voxora", "downloaded_tracks.json")
}

var trackURIRegex = regexp.MustCompile(`spotify:track:[A-Za-z0-9]+`)

func extractTrackURIFromMediaTags(filePath string) string {
	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "format_tags=comment,description",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	match := trackURIRegex.FindString(string(out))
	return strings.TrimSpace(match)
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
	result := make(map[string]string)
	dir := strings.TrimSpace(downloadDir)
	if dir == "" {
		return result
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !isScannableAudioFile(path) {
			return nil
		}

		uri := extractTrackURIFromMediaTags(path)
		if uri == "" {
			return nil
		}
		result[uri] = path
		return nil
	})

	return result
}

func (b *SpotifyBridge) loadDownloadedIndex() {
	path := downloadIndexPath()
	clean := make(map[string]string)
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path)
		if err == nil {
			var idx downloadedTrackIndex
			if json.Unmarshal(raw, &idx) == nil {
				for uri, filePath := range idx.Tracks {
					u := strings.TrimSpace(uri)
					p := strings.TrimSpace(filePath)
					if u == "" || p == "" {
						continue
					}
					if _, statErr := os.Stat(p); statErr == nil {
						clean[u] = p
					}
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			// ignore malformed/unreadable index and continue with disk scan.
		}
	}

	for uri, filePath := range scanDownloadedAudioTags(defaultDownloadDir()) {
		clean[uri] = filePath
	}

	b.mu.Lock()
	b.downloadedByURI = clean
	b.mu.Unlock()

	_ = b.saveDownloadedIndex()
}

func (b *SpotifyBridge) saveDownloadedIndex() error {
	path := downloadIndexPath()
	if strings.TrimSpace(path) == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b.mu.Lock()
	copyMap := make(map[string]string, len(b.downloadedByURI))
	for uri, filePath := range b.downloadedByURI {
		copyMap[uri] = filePath
	}
	b.mu.Unlock()

	encoded, err := json.MarshalIndent(downloadedTrackIndex{Tracks: copyMap}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, encoded, 0o644)
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
		}
		b.mu.Unlock()
		_ = b.saveDownloadedIndex()
	}
	return out
}

func (b *SpotifyBridge) publishTrackList(items []libspotdl.LibraryTrackSummary) {
	tracksJSON := "[]"
	if encoded, encErr := json.Marshal(items); encErr == nil {
		tracksJSON = string(encoded)
	}
	b.set("trackListJson", tracksJSON)
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
		downloadedPath := strings.TrimSpace(results[0].OutputPath)
		b.mu.Lock()
		if b.downloadedByURI == nil {
			b.downloadedByURI = make(map[string]string)
		}
		b.downloadedByURI[trackURI] = downloadedPath
		for i := range b.trackItems {
			if b.trackItems[i].URI == trackURI {
				b.trackItems[i].Downloaded = true
				b.trackItems[i].DownloadedPath = downloadedPath
				break
			}
		}
		updatedTracks := append([]libspotdl.LibraryTrackSummary(nil), b.trackItems...)
		b.mu.Unlock()
		_ = b.saveDownloadedIndex()
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
