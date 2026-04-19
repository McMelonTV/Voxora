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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	libspotdl "github.com/McMelonTV/Voxora/libspotd"
	"github.com/McMelonTV/Voxora/voxora/internal/appapi"
	"github.com/dhowden/tag"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

type SpotifyBridge struct {
	props           *qml.QQmlPropertyMap
	uiSignalMappers []*qt.QSignalMapper
	uiRoot          *qt.QObject

	sessionMgr  *appapi.SessionManager
	navMgr      *appapi.NavigationStackManager
	trackModel  *appapi.TrackModelManager
	playbackMgr *appapi.PlaybackManager
	streamMgr   *appapi.StreamManager

	mu         sync.Mutex
	flow       *libspotdl.InteractiveAuthFlow
	flowCtx    context.Context
	flowCancel context.CancelFunc

	trackContextURI      string
	trackPageOffset      int
	trackPageLimit       int
	trackItems           []libspotdl.LibraryTrackSummary
	loadingTracks        bool
	resumeActive         bool
	resumeTargetTrackURI string
	resumeTargetPosMs    int

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

	streamHTTPListener     net.Listener
	streamHTTPServer       *http.Server
	streamHTTPBaseURL      string
	debugBridgeProps       bool
	debugPlayback          bool
	sessionLoadedSeq       int
	actionSeq              int
	playbackIntentSeq      int
	seekDecisionSeq        int
	openCollectionReqSeq   int
	streamTrackReqSeq      int
	startPlaybackReqSeq    int
	playAdjacentReqSeq     int
	computePrebufferReqSeq int
	navOpenReqSeq          int
	navBackReqSeq          int
	clearSessionReqSeq     int
	loadMoreTracksReqSeq   int
	prebufferTracksReqSeq  int
}

func NewSpotifyBridge() *SpotifyBridge {
	props := qml.NewQQmlPropertyMap()
	b := &SpotifyBridge{
		props:              props,
		sessionMgr:         appapi.NewSessionManager(),
		navMgr:             appapi.NewNavigationStackManager(),
		trackModel:         appapi.NewTrackModelManager(),
		playbackMgr:        appapi.NewPlaybackManager(),
		streamMgr:          appapi.NewStreamManager(),
		downloadedByURI:    make(map[string]string),
		downloadedArtByURI: make(map[string]string),
	}
	b.debugBridgeProps = strings.EqualFold(strings.TrimSpace(os.Getenv("VOXORA_DEBUG_BRIDGE_PROPS")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("VOXORA_DEBUG_BRIDGE_PROPS")), "true")
	debugPlaybackEnv := strings.TrimSpace(os.Getenv("VOXORA_DEBUG_PLAYBACK"))
	b.debugPlayback = strings.EqualFold(debugPlaybackEnv, "1") || strings.EqualFold(debugPlaybackEnv, "true")
	b.startStreamHTTPServer()

	b.set("statusText", "Spotify auth: idle")
	b.set("isBusy", false)
	b.set("lastError", "")
	b.set("likedSongsCount", 0)
	b.set("likedSongsName", "Liked Songs")
	b.set("playlistsJson", "[]")
	b.set("libraryStatus", "Spotify library: not loaded")
	b.set("viewMode", "library")
	b.set("trackListJson", "[]")
	b.set("trackListTitle", "")
	b.set("trackListStatus", "")
	b.set("isLoadingTracks", false)
	b.set("trackHasMore", false)
	b.set("isDownloadingTrack", false)
	b.set("downloadTrackRequest", "")
	b.set("isStreamingTrack", false)
	b.set("streamTrackRequest", "")
	b.set("clearPrebufferNonce", 0)
	b.set("streamPlayPath", "")
	b.set("streamPlayReadyNonce", 0)
	b.set("streamCacheReady", false)
	b.set("streamCachePath", "")
	b.set("streamCacheFilePath", "")
	b.set("streamBufferedBytes", int64(0))
	b.set("streamBufferedTotal", int64(0))
	b.set("clearStreamNonce", 0)
	b.set("clearStreamCacheAllNonce", 0)
	b.set("uiIntentRequest", "")
	b.set("startAuthNonce", 0)
	b.set("loadSessionNonce", 0)
	b.set("saveSessionNonce", 0)
	b.set("clearSessionNonce", 0)
	b.set("sessionContextURI", "")
	b.set("sessionContextName", "")
	b.set("sessionTrackURI", "")
	b.set("sessionTrackName", "")
	b.set("sessionTrackArtist", "")
	b.set("sessionAlbumArtURL", "")
	b.set("sessionPositionMs", 0)
	b.set("sessionUserVolume", 0.8)
	b.set("sessionDataSavingMode", false)
	b.set("sessionLoadedNonce", 0)
	b.set("navDepth", 0)
	b.set("navCanGoBack", false)
	b.set("activeContextURI", "")
	b.set("activeContextName", "")
	b.set("trackModelVersion", 0)
	b.set("trackModelCount", 0)
	b.set("currentTrackIndex", -1)
	b.set("currentTrackURI", "")
	b.set("startPlaybackRequest", "")
	b.set("playAdjacentRequest", "")
	b.set("openCollectionRequest", "")
	b.set("navOpenRequest", "")
	b.set("navigateBackNonce", 0)
	b.set("navBackNonce", 0)
	b.set("loadMoreTracksNonce", 0)
	b.set("prebufferTracksRequest", "")
	b.set("computePrebufferNonce", 0)
	b.set("computePrebufferRequest", "")
	b.set("evaluateSeekRequest", "")
	b.set("evaluateRecoveryRequest", "")
	b.set("seekAllowed", false)
	b.set("seekDenyReason", "")
	b.set("seekDecisionNonce", 0)
	b.set("playbackIntentJson", "")
	b.set("playbackIntentNonce", 0)
	b.set("actionKind", "")
	b.set("actionReason", "")
	b.set("actionTargetPath", "")
	b.set("actionTargetPosMs", 0)
	b.set("actionTargetRatio", 0.0)
	b.set("actionTrackIndex", -1)
	b.set("actionShouldPlay", true)
	b.set("actionTrackName", "")
	b.set("actionTrackURI", "")
	b.set("actionTrackArtistText", "")
	b.set("actionTrackAlbumArtURL", "")
	b.set("actionTrackDownloadedPath", "")
	b.set("actionTrackDurationMs", 0)
	b.set("actionNonce", 0)

	b.publishSessionState()
	b.publishNavigationState()
	b.publishTrackModelState()

	props.OnValueChanged(func(key string, value *qt.QVariant) {
		if b.debugBridgeProps {
			fmt.Printf("[voxora][bridge] valueChanged key=%s value=%q\n", key, strings.TrimSpace(value.ToString()))
		}
		switch key {
		case "startAuthNonce":
			go b.startFlow()
		case "uiIntentRequest":
			go b.handleUIIntentRequest(value.ToString())
		case "downloadTrackRequest":
			go b.downloadTrack(value.ToString())
		case "streamTrackRequest":
			go b.streamTrack(value.ToString())
		case "clearPrebufferNonce":
			b.clearPrebuffer()
		case "clearStreamNonce":
			go b.clearStreamCache()
		case "clearStreamCacheAllNonce":
			go b.clearAllStreamCache()
		case "loadSessionNonce":
			go b.loadSessionState()
		case "saveSessionNonce":
			go b.saveSessionStateFromBridge()
		case "clearSessionNonce":
			go b.clearSessionState()
		case "openCollectionRequest":
			go b.handleLegacyOpenCollectionRequest(value.ToString())
		case "navOpenRequest":
			go b.handleLegacyOpenCollectionRequest(value.ToString())
		case "navigateBackNonce", "navBackNonce":
			go b.handleLegacyNavigateBackNonce(key, value.ToString())
		case "loadMoreTracksNonce":
			go b.handleLegacyLoadMoreTracksNonce(value.ToString())
		case "prebufferTracksRequest":
			go b.handleLegacyPrebufferTracksRequest(value.ToString())
		case "computePrebufferNonce":
			go b.handleLegacyComputePrebufferNonce(value.ToString())
		case "startPlaybackRequest":
			b.handleStartPlaybackRequest(value.ToString())
		case "playAdjacentRequest":
			b.handlePlayAdjacentRequest(value.ToString())
		case "computePrebufferRequest":
			go b.handleComputePrebufferRequest(value.ToString())
		case "evaluateSeekRequest":
			b.handleEvaluateSeekRequest(value.ToString())
		case "evaluateRecoveryRequest":
			b.handleEvaluateRecoveryRequest(value.ToString())
		}
	})

	b.loadDownloadedIndex()

	go b.bootstrapExistingAuth()

	return b
}

func (b *SpotifyBridge) handleUIIntentRequest(raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return
	}

	parts := strings.Split(trimmed, "\n")
	if len(parts) == 0 {
		return
	}

	seq := 0
	if len(parts) >= 2 {
		if parsedSeq, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			seq = parsedSeq
			parts = parts[:len(parts)-1]
		}
	}
	if !b.acceptRequestSeq("uiIntent", seq) {
		return
	}

	kind := strings.TrimSpace(parts[0])
	switch kind {
	case "open_collection":
		if len(parts) < 3 {
			return
		}
		contextURI := strings.TrimSpace(parts[1])
		title := strings.TrimSpace(parts[2])
		if contextURI == "" {
			return
		}
		if title == "" {
			title = "Tracks"
		}
		b.clearResumePending()
		fromViewMode := strings.TrimSpace(b.props.Value("viewMode").ToString())
		b.openCollectionContext(contextURI, title, fromViewMode)
	case "navigate_back":
		if b.navMgr.Back() {
			b.publishNavigationState()
		}
		b.set("viewMode", "library")
		b.set("trackListStatus", "")
	case "load_more_tracks":
		b.loadMoreTracks()
	}
}

func (b *SpotifyBridge) clearSessionState() {
	if b.sessionMgr == nil {
		return
	}
	if err := b.sessionMgr.Clear(); err != nil {
		b.set("trackListStatus", "Session clear failed: "+err.Error())
		return
	}
	b.publishSessionState()
	b.set("sessionLoadedNonce", 0)
	b.set("trackListStatus", "Session cleared")
}

func (b *SpotifyBridge) AttachUISignals(root *qt.QObject) {
	if root == nil {
		return
	}
	b.uiRoot = root

	hook := func(signalName string) {
		if strings.TrimSpace(signalName) == "" {
			return
		}
		mapper := qt.NewQSignalMapper2(root)
		b.uiSignalMappers = append(b.uiSignalMappers, mapper)
		mapper.OnMappedString(func(name string) {
			b.handleUISignal(strings.TrimSpace(name))
		})
		mapper.SetMapping2(root, signalName)
		mapper.Connect2(root, "2"+signalName+"()", "1map()")
	}

	hook("uiConnectSpotifyRequested")
	hook("uiClearStreamCacheRequested")
	hook("uiVolumeChangedRequested")
	hook("uiDataSavingToggledRequested")
	hook("uiOpenCollectionRequested")
	hook("uiNavigateBackRequested")
	hook("uiLoadMoreRequested")
	hook("uiPlayTrackRequested")
	hook("uiDownloadTrackRequested")
	hook("uiPreviousRequested")
	hook("uiNextRequested")
	hook("uiLoadSessionRequested")
	hook("uiPersistSessionRequested")
	hook("uiSeekRequested")
	hook("uiClearStreamRequested")
	hook("uiStreamTrackRequested")
	hook("uiComputePrebufferRequested")
	hook("uiEvaluateRecoveryRequested")

	go b.loadSessionState()
}

func (b *SpotifyBridge) uiPropString(name string) string {
	if b.uiRoot == nil {
		return ""
	}
	v := b.uiRoot.Property(name)
	if v == nil {
		return ""
	}
	return strings.TrimSpace(v.ToString())
}

func (b *SpotifyBridge) uiPropInt(name string, fallback int) int {
	raw := b.uiPropString(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func (b *SpotifyBridge) uiPropInt64(name string, fallback int64) int64 {
	raw := b.uiPropString(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func (b *SpotifyBridge) uiPropFloat(name string, fallback float64) float64 {
	raw := b.uiPropString(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return v
}

func (b *SpotifyBridge) uiPropBool(name string, fallback bool) bool {
	raw := b.uiPropString(name)
	if raw == "" {
		return fallback
	}
	return strings.EqualFold(raw, "true")
}

func (b *SpotifyBridge) persistSessionFromUIRoot() {
	if b.sessionMgr == nil || b.uiRoot == nil {
		return
	}
	ctxURI := b.uiPropString("activeContextURI")
	trackURI := b.uiPropString("currentTrackURI")
	if ctxURI == "" || trackURI == "" {
		return
	}
	b.sessionMgr.Update(func(s *appapi.SessionState) {
		s.ContextURI = ctxURI
		s.ContextName = b.uiPropString("activeContextName")
		s.TrackURI = trackURI
		s.TrackName = b.uiPropString("currentTrackTitle")
		s.TrackArtist = b.uiPropString("currentTrackArtist")
		s.AlbumArtURL = b.uiPropString("currentTrackAlbumArtUrl")
		s.PositionMs = b.uiPropInt("sessionPendingPositionMs", 0)
		s.UserVolume = b.uiPropFloat("userVolume", 0.8)
		s.DataSavingMode = b.uiPropBool("dataSavingMode", false)
	})
	if err := b.sessionMgr.Save(); err != nil {
		b.set("trackListStatus", "Session save failed: "+err.Error())
		return
	}
	b.publishSessionState()
}

func (b *SpotifyBridge) handleUISignal(name string) {
	switch name {
	case "uiConnectSpotifyRequested":
		go b.startFlow()
	case "uiClearStreamCacheRequested":
		go b.clearAllStreamCache()
	case "uiVolumeChangedRequested":
		v := b.uiPropFloat("uiPendingVolume", 0.8)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		b.set("sessionUserVolume", v)
		go b.saveSessionStateFromBridge()
	case "uiDataSavingToggledRequested":
		next := b.uiPropBool("uiPendingDataSavingMode", false)
		b.set("sessionDataSavingMode", next)
		go b.saveSessionStateFromBridge()
		if next {
			b.clearPrebuffer()
		}
	case "uiOpenCollectionRequested":
		uri := b.uiPropString("uiPendingCollectionURI")
		name := b.uiPropString("uiPendingCollectionName")
		if uri == "" {
			return
		}
		if name == "" {
			name = "Collection"
		}
		b.clearResumePending()
		fromViewMode := b.uiPropString("viewMode")
		b.openCollectionContext(uri, name, fromViewMode)
	case "uiNavigateBackRequested":
		if b.navMgr.Back() {
			b.publishNavigationState()
		}
		b.set("viewMode", "library")
		b.set("trackListStatus", "")
	case "uiLoadMoreRequested":
		b.loadMoreTracks()
	case "uiLoadSessionRequested":
		go b.loadSessionState()
	case "uiPersistSessionRequested":
		b.persistSessionFromUIRoot()
	case "uiPlayTrackRequested":
		idx := b.uiPropInt("uiPendingTrackIndex", -1)
		if idx < 0 {
			return
		}
		b.handleStartPlaybackRequest(strconv.Itoa(idx) + "\ntrue\n0")
	case "uiSeekRequested":
		ratio := b.uiPropFloat("uiPendingSeekRatio", -1)
		if ratio < 0 {
			return
		}
		durationMs := b.uiPropInt("effectiveDurationMs", 0)
		currentPath := b.uiPropString("currentPlayingPath")
		isPartStream := strings.EqualFold(strconv.FormatBool(strings.Contains(currentPath, ".stream") || strings.Contains(currentPath, "/stream?kind=play&")), "true")
		seekEnabled := b.uiPropBool("seekEnabledForCurrentSource", true)
		cacheReady := b.uiPropBool("streamCacheReady", false)
		cachePath := strings.TrimSpace(b.props.Value("streamCacheFilePath").ToString())
		if cachePath == "" {
			cachePath = strings.TrimSpace(b.props.Value("streamCachePath").ToString())
		}
		bufferedBytes := b.uiPropInt64("streamBufferedBytes", 0)
		if bufferedBytes == 0 {
			bufferedBytes = int64(mustAtoiDefault(strings.TrimSpace(b.props.Value("streamBufferedBytes").ToString()), 0))
		}
		bufferedTotal := b.uiPropInt64("streamBufferedTotal", 0)
		if bufferedTotal == 0 {
			bufferedTotal = int64(mustAtoiDefault(strings.TrimSpace(b.props.Value("streamBufferedTotal").ToString()), 0))
		}
		b.handleEvaluateSeekRequest(strings.Join([]string{
			strconv.FormatFloat(ratio, 'f', -1, 64),
			strconv.Itoa(durationMs),
			currentPath,
			strconv.FormatBool(isPartStream),
			strconv.FormatBool(seekEnabled),
			strconv.FormatBool(cacheReady),
			cachePath,
			strconv.FormatInt(bufferedBytes, 10),
			strconv.FormatInt(bufferedTotal, 10),
		}, "\n"))
	case "uiClearStreamRequested":
		go b.clearStreamCache()
	case "uiStreamTrackRequested":
		uri := b.uiPropString("uiPendingStreamTrackURI")
		trackName := b.uiPropString("uiPendingStreamTrackName")
		if uri == "" {
			return
		}
		if trackName == "" {
			trackName = "Track"
		}
		go b.streamTrack(uri + "\n" + trackName)
	case "uiComputePrebufferRequested":
		idx := b.uiPropInt("currentTrackIndex", -1)
		ahead := b.uiPropInt("prebufferAheadCount", 3)
		autoplay := b.uiPropBool("autoplayEnabled", true)
		dataSaving := b.uiPropBool("dataSavingMode", false)
		if idx < 0 {
			return
		}
		go b.handleComputePrebufferRequest(strings.Join([]string{
			strconv.Itoa(idx),
			strconv.Itoa(ahead),
			strconv.FormatBool(autoplay),
			strconv.FormatBool(dataSaving),
			"0",
		}, "\n"))
	case "uiEvaluateRecoveryRequested":
		cachePath := strings.TrimSpace(b.props.Value("streamCacheFilePath").ToString())
		if cachePath == "" {
			cachePath = strings.TrimSpace(b.props.Value("streamCachePath").ToString())
		}
		b.handleEvaluateRecoveryRequest(strings.Join([]string{
			strconv.FormatBool(b.uiPropBool("manualStopRequested", false)),
			strconv.FormatBool(b.uiPropBool("streamRecovering", false)),
			strconv.FormatBool(b.uiPropBool("holdStoppedTrackState", false)),
			strconv.FormatBool(b.uiPropBool("streamSourceSwitching", false)),
			b.uiPropString("currentPlayingPath"),
			cachePath,
			strings.TrimSpace(b.props.Value("streamPlayPath").ToString()),
			strconv.FormatBool(b.uiPropBool("uiPendingLikelyNaturalEnd", false)),
			strconv.FormatBool(b.uiPropBool("uiPendingHasNewBufferedData", false)),
			strconv.FormatBool(strings.EqualFold(strings.TrimSpace(b.props.Value("isStreamingTrack").ToString()), "true")),
			strconv.Itoa(b.uiPropInt("streamRecoverAttempts", 0)),
		}, "\n"))
	case "uiDownloadTrackRequested":
		uri := b.uiPropString("uiPendingDownloadURI")
		name := b.uiPropString("uiPendingDownloadName")
		if uri == "" {
			return
		}
		if name == "" {
			name = "Track"
		}
		go b.downloadTrack(uri + "\n" + name)
	case "uiPreviousRequested":
		current := b.uiPropInt("currentTrackIndex", -1)
		if current < 0 {
			return
		}
		b.handlePlayAdjacentRequest(strconv.Itoa(current) + "\n-1\ntrue\n0")
	case "uiNextRequested":
		current := b.uiPropInt("currentTrackIndex", -1)
		if current < 0 {
			return
		}
		b.handlePlayAdjacentRequest(strconv.Itoa(current) + "\n1\ntrue\n0")
	}
}

func (b *SpotifyBridge) handleLegacyOpenCollectionRequest(raw string) {
	parts := strings.SplitN(strings.TrimSpace(raw), "\n", 3)
	if len(parts) < 2 {
		return
	}
	contextURI := strings.TrimSpace(parts[0])
	title := strings.TrimSpace(parts[1])
	seq := 0
	if len(parts) >= 3 {
		seq, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
	}
	if seq > 0 {
		if !b.acceptRequestSeq("legacyNavOpen", seq) {
			return
		}
	}
	if contextURI == "" {
		return
	}
	if title == "" {
		title = "Tracks"
	}
	b.clearResumePending()
	fromViewMode := strings.TrimSpace(b.props.Value("viewMode").ToString())
	b.openCollectionContext(contextURI, title, fromViewMode)
}

func (b *SpotifyBridge) handleLegacyNavigateBackNonce(kind, raw string) {
	seq, _ := strconv.Atoi(strings.TrimSpace(raw))
	if seq > 0 {
		if !b.acceptRequestSeq(kind, seq) {
			return
		}
	}
	if b.navMgr.Back() {
		b.publishNavigationState()
	}
	b.set("viewMode", "library")
	b.set("trackListStatus", "")
}

func (b *SpotifyBridge) handleLegacyLoadMoreTracksNonce(raw string) {
	seq, _ := strconv.Atoi(strings.TrimSpace(raw))
	if seq > 0 {
		if !b.acceptRequestSeq("loadMoreTracksNonce", seq) {
			return
		}
	}
	b.loadMoreTracks()
}

func (b *SpotifyBridge) handleLegacyPrebufferTracksRequest(raw string) {
	parts := strings.Split(strings.TrimSpace(raw), "\n")
	if len(parts) == 0 {
		return
	}
	if len(parts) >= 2 {
		if seq, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			if !b.acceptRequestSeq("prebufferTracksRequest", seq) {
				return
			}
			parts = parts[:len(parts)-1]
		}
	}
	b.prebufferTracks(strings.Join(parts, "\n"))
}

func (b *SpotifyBridge) handleLegacyComputePrebufferNonce(raw string) {
	seq, _ := strconv.Atoi(strings.TrimSpace(raw))
	if seq > 0 {
		if !b.acceptRequestSeq("computePrebufferNonce", seq) {
			return
		}
	}
	b.handleComputePrebufferRequest(strings.Join([]string{
		strings.TrimSpace(b.props.Value("currentTrackIndex").ToString()),
		"3",
		"true",
		"false",
		strconv.Itoa(seq),
	}, "\n"))
}

func (b *SpotifyBridge) publishSessionState() {
	if b.sessionMgr == nil {
		return
	}
	s := b.sessionMgr.State()
	b.set("sessionContextURI", s.ContextURI)
	b.set("sessionContextName", s.ContextName)
	b.set("sessionTrackURI", s.TrackURI)
	b.set("sessionTrackName", s.TrackName)
	b.set("sessionTrackArtist", s.TrackArtist)
	b.set("sessionAlbumArtURL", b.normalizeSessionAlbumArtURL(strings.TrimSpace(s.TrackURI), strings.TrimSpace(s.AlbumArtURL)))
	b.set("sessionPositionMs", s.PositionMs)
	b.set("sessionUserVolume", s.UserVolume)
	b.set("sessionDataSavingMode", s.DataSavingMode)
}

func (b *SpotifyBridge) normalizeSessionAlbumArtURL(trackURI, rawURL string) string {
	uri := strings.TrimSpace(trackURI)
	raw := strings.TrimSpace(rawURL)

	// Prefer embedded artwork for downloaded tracks when available.
	if uri != "" {
		b.mu.Lock()
		embedded := strings.TrimSpace(b.downloadedArtByURI[uri])
		b.mu.Unlock()
		if embedded != "" {
			if local := b.embeddedArtworkHTTPURL(uri); local != "" {
				return local
			}
		}
	}

	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err == nil {
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host == "127.0.0.1" || host == "localhost" {
			if parsed.Path == "/art" {
				q := parsed.Query()
				if qURI := strings.TrimSpace(q.Get("uri")); qURI != "" {
					if local := b.embeddedArtworkHTTPURL(qURI); local != "" {
						return local
					}
				}
				if qURL := strings.TrimSpace(q.Get("url")); qURL != "" {
					if proxied := b.artworkHTTPURL(qURL); proxied != "" {
						return proxied
					}
				}
			}
			// Any other stale localhost URL should not be reused.
			return ""
		}
	}

	if proxied := b.artworkHTTPURL(raw); proxied != "" {
		return proxied
	}
	return raw
}

func (b *SpotifyBridge) publishNavigationState() {
	if b.navMgr == nil {
		return
	}
	state := b.navMgr.State()
	b.set("navDepth", len(state.Stack))
	b.set("navCanGoBack", len(state.Stack) > 0)
	b.set("activeContextURI", state.ActiveContextURI)
	b.set("activeContextName", state.ActiveContextName)
}

func (b *SpotifyBridge) publishTrackModelState() {
	if b.trackModel == nil {
		return
	}
	s := b.trackModel.Snapshot()
	b.set("trackModelVersion", s.Version)
	b.set("trackModelCount", s.Count)
}

func (b *SpotifyBridge) publishPlaybackAction(intent appapi.PlaybackIntent) {
	b.logPlaybackf("publish index=%d shouldPlay=%t uri=%q downloaded=%t durationMs=%d", intent.Index, intent.ShouldPlay, intent.URI, strings.TrimSpace(intent.DownloadedPath) != "", intent.DurationMs)
	b.set("currentTrackIndex", intent.Index)
	b.set("currentTrackURI", strings.TrimSpace(intent.URI))
	b.mu.Lock()
	b.playbackIntentSeq += 1
	playbackNonce := b.playbackIntentSeq
	b.mu.Unlock()
	if payload, err := json.Marshal(intent); err == nil {
		b.set("playbackIntentJson", string(payload))
	} else {
		b.set("playbackIntentJson", "")
	}
	b.set("playbackIntentNonce", playbackNonce)
	b.publishAction("playback_select", "", "", 0, 0, intent)
}

func (b *SpotifyBridge) playbackRowsForIntent() []appapi.TrackRow {
	if b.trackModel == nil {
		b.logPlaybackf("no rows available (trackModel unavailable)")
		return nil
	}

	rows := b.trackModel.Rows()
	if len(rows) == 0 {
		b.logPlaybackf("no rows available (trackModel empty)")
		return nil
	}
	b.logPlaybackf("using trackModel rows=%d", len(rows))
	return rows
}

func (b *SpotifyBridge) handleStartPlaybackRequest(raw string) {
	if b.playbackMgr == nil {
		return
	}
	b.logPlaybackf("start request raw=%q", raw)
	parts := strings.SplitN(raw, "\n", 3)
	if len(parts) < 2 {
		b.logPlaybackf("start request invalid payload")
		b.publishAction("playback_error", "invalid startPlaybackRequest", "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	if len(parts) >= 3 {
		if seq, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
			if !b.acceptRequestSeq("startPlayback", seq) {
				return
			}
		}
	}
	idx, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		b.logPlaybackf("start request invalid index=%q err=%v", strings.TrimSpace(parts[0]), err)
		b.publishAction("playback_error", "invalid playback index", "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	shouldPlay := strings.EqualFold(strings.TrimSpace(parts[1]), "true")
	rows := b.playbackRowsForIntent()
	b.logPlaybackf("start resolve index=%d shouldPlay=%t rows=%d", idx, shouldPlay, len(rows))
	intent, err := b.playbackMgr.StartAtIndex(rows, idx, shouldPlay)
	if err != nil {
		b.logPlaybackf("start resolve error: %v", err)
		b.publishAction("playback_error", err.Error(), "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	b.publishPlaybackAction(intent)
}

func (b *SpotifyBridge) handlePlayAdjacentRequest(raw string) {
	if b.playbackMgr == nil {
		return
	}
	b.logPlaybackf("adjacent request raw=%q", raw)
	parts := strings.SplitN(raw, "\n", 4)
	if len(parts) < 3 {
		b.logPlaybackf("adjacent request invalid payload")
		b.publishAction("playback_error", "invalid playAdjacentRequest", "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	if len(parts) >= 4 {
		if seq, err := strconv.Atoi(strings.TrimSpace(parts[3])); err == nil {
			if !b.acceptRequestSeq("playAdjacent", seq) {
				return
			}
		}
	}
	currentIndex, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		b.logPlaybackf("adjacent invalid current index=%q err=%v", strings.TrimSpace(parts[0]), err)
		b.publishAction("playback_error", "invalid current index", "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	step, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		b.logPlaybackf("adjacent invalid step=%q err=%v", strings.TrimSpace(parts[1]), err)
		b.publishAction("playback_error", "invalid adjacent step", "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	shouldPlay := strings.EqualFold(strings.TrimSpace(parts[2]), "true")
	rows := b.playbackRowsForIntent()
	b.logPlaybackf("adjacent resolve current=%d step=%d shouldPlay=%t rows=%d", currentIndex, step, shouldPlay, len(rows))
	intent, err := b.playbackMgr.Adjacent(rows, currentIndex, step, shouldPlay)
	if err != nil {
		b.logPlaybackf("adjacent resolve error: %v", err)
		b.publishAction("playback_error", err.Error(), "", 0, 0, appapi.PlaybackIntent{})
		return
	}
	b.publishPlaybackAction(intent)
}

func (b *SpotifyBridge) logPlaybackf(format string, args ...any) {
	if !b.debugPlayback {
		return
	}
	fmt.Printf("[voxora][playback-intent] "+format+"\n", args...)
}

func (b *SpotifyBridge) handleComputePrebufferRequest(raw string) {
	if b.playbackMgr == nil {
		return
	}
	parts := strings.SplitN(raw, "\n", 5)
	if len(parts) < 4 {
		return
	}
	if len(parts) >= 5 {
		if seq, err := strconv.Atoi(strings.TrimSpace(parts[4])); err == nil {
			if !b.acceptRequestSeq("computePrebuffer", seq) {
				return
			}
		}
	}
	currentIndex, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return
	}
	aheadCount, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return
	}
	autoplayEnabled := strings.EqualFold(strings.TrimSpace(parts[2]), "true")
	dataSavingMode := strings.EqualFold(strings.TrimSpace(parts[3]), "true")

	if !autoplayEnabled || dataSavingMode {
		return
	}

	rows := b.playbackRowsForIntent()
	queue := b.playbackMgr.ComputePrebufferQueue(rows, currentIndex, aheadCount)
	if len(queue) == 0 {
		return
	}
	b.prebufferTracks(strings.Join(queue, "\n"))
}

func (b *SpotifyBridge) handleEvaluateSeekRequest(raw string) {
	if b.streamMgr == nil {
		return
	}
	parts := strings.SplitN(raw, "\n", 10)
	if len(parts) < 9 {
		b.publishSeekAction(nil, "invalid evaluateSeekRequest")
		return
	}

	ratio, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		b.publishSeekAction(nil, "invalid seek ratio")
		return
	}
	durationMs, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		b.publishSeekAction(nil, "invalid effective duration")
		return
	}
	currentPath := strings.TrimSpace(parts[2])
	isPartStream := strings.EqualFold(strings.TrimSpace(parts[3]), "true")
	seekEnabled := strings.EqualFold(strings.TrimSpace(parts[4]), "true")
	cacheReady := strings.EqualFold(strings.TrimSpace(parts[5]), "true")
	cachePath := strings.TrimSpace(parts[6])
	bufferedBytes, err := strconv.ParseInt(strings.TrimSpace(parts[7]), 10, 64)
	if err != nil {
		b.publishSeekAction(nil, "invalid buffered bytes")
		return
	}
	bufferedTotal, err := strconv.ParseInt(strings.TrimSpace(parts[8]), 10, 64)
	if err != nil {
		b.publishSeekAction(nil, "invalid buffered total")
		return
	}

	decision := b.streamMgr.EvaluateSeek(appapi.SeekDecisionInput{
		Ratio:                ratio,
		EffectiveDurationMs:  durationMs,
		CurrentPlayingPath:   currentPath,
		IsPartStreamSource:   isPartStream,
		SeekEnabledForSource: seekEnabled,
		StreamCacheReady:     cacheReady,
		StreamCachePath:      cachePath,
		StreamBufferedBytes:  bufferedBytes,
		StreamBufferedTotal:  bufferedTotal,
	})

	b.publishSeekAction(&decision, "")
}

func (b *SpotifyBridge) publishSeekAction(decision *appapi.SeekDecision, errMsg string) {
	if decision == nil {
		reason := strings.TrimSpace(errMsg)
		if reason == "" {
			reason = "Seek decision unavailable"
		}
		b.set("trackListStatus", reason)
		b.mu.Lock()
		b.seekDecisionSeq += 1
		nonce := b.seekDecisionSeq
		b.mu.Unlock()
		b.set("seekAllowed", false)
		b.set("seekDenyReason", reason)
		b.set("seekDecisionNonce", nonce)
		b.publishAction("seek_denied", reason, "", 0, 0, appapi.PlaybackIntent{})
		return
	}

	if !decision.Allow {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "Seek denied"
		}
		b.set("trackListStatus", reason)
		b.mu.Lock()
		b.seekDecisionSeq += 1
		nonce := b.seekDecisionSeq
		b.mu.Unlock()
		b.set("seekAllowed", false)
		b.set("seekDenyReason", reason)
		b.set("seekDecisionNonce", nonce)
		b.publishAction("seek_denied", reason, "", 0, 0, appapi.PlaybackIntent{})
		return
	}

	b.mu.Lock()
	b.seekDecisionSeq += 1
	nonce := b.seekDecisionSeq
	b.mu.Unlock()
	b.set("seekAllowed", true)
	b.set("seekDenyReason", "")
	b.set("seekDecisionNonce", nonce)

	if decision.ShouldSwitchToCache {
		b.publishAction("seek_switch_cache", "", strings.TrimSpace(decision.TargetPath), decision.TargetPosMs, decision.TargetRatio, appapi.PlaybackIntent{})
		return
	}

	b.publishAction("seek_apply", "", "", decision.TargetPosMs, decision.TargetRatio, appapi.PlaybackIntent{})
}

func (b *SpotifyBridge) publishAction(kind, reason, targetPath string, targetPosMs int, targetRatio float64, playback appapi.PlaybackIntent) {
	b.mu.Lock()
	b.actionSeq += 1
	nonce := b.actionSeq
	b.mu.Unlock()

	b.set("actionKind", strings.TrimSpace(kind))
	b.set("actionReason", strings.TrimSpace(reason))
	b.set("actionTargetPath", strings.TrimSpace(targetPath))
	b.set("actionTargetPosMs", targetPosMs)
	b.set("actionTargetRatio", targetRatio)
	b.set("actionTrackIndex", playback.Index)
	b.set("actionShouldPlay", playback.ShouldPlay)
	b.set("actionTrackName", playback.Name)
	b.set("actionTrackURI", playback.URI)
	b.set("actionTrackArtistText", playback.ArtistText)
	b.set("actionTrackAlbumArtURL", playback.AlbumArtURL)
	b.set("actionTrackDownloadedPath", playback.DownloadedPath)
	b.set("actionTrackDurationMs", playback.DurationMs)
	b.set("actionNonce", nonce)

	if strings.TrimSpace(kind) == "playback_error" {
		r := strings.TrimSpace(reason)
		if r != "" {
			b.set("trackListStatus", r)
		}
	}
}

func (b *SpotifyBridge) handleEvaluateRecoveryRequest(raw string) {
	if b.streamMgr == nil {
		return
	}
	parts := strings.SplitN(raw, "\n", 12)
	if len(parts) < 11 {
		return
	}
	decision := b.streamMgr.EvaluateRecovery(appapi.RecoveryDecisionInput{
		ManualStopRequested:   strings.EqualFold(strings.TrimSpace(parts[0]), "true"),
		StreamRecovering:      strings.EqualFold(strings.TrimSpace(parts[1]), "true"),
		HoldStoppedTrackState: strings.EqualFold(strings.TrimSpace(parts[2]), "true"),
		StreamSourceSwitching: strings.EqualFold(strings.TrimSpace(parts[3]), "true"),
		CurrentPlayingPath:    strings.TrimSpace(parts[4]),
		CachePath:             strings.TrimSpace(parts[5]),
		StreamPlayPath:        strings.TrimSpace(parts[6]),
		LikelyNaturalEnd:      strings.EqualFold(strings.TrimSpace(parts[7]), "true"),
		HasNewBufferedData:    strings.EqualFold(strings.TrimSpace(parts[8]), "true"),
		StillDownloading:      strings.EqualFold(strings.TrimSpace(parts[9]), "true"),
		StreamRecoverAttempts: mustAtoiDefault(strings.TrimSpace(parts[10]), 0),
	})

	if decision.ShouldRecover {
		b.publishAction("recover_stream", strings.TrimSpace(decision.Reason), "", 0, 0, appapi.PlaybackIntent{})
	} else {
		b.publishAction("recover_skip", strings.TrimSpace(decision.Reason), "", 0, 0, appapi.PlaybackIntent{})
	}
}

func mustAtoiDefault(raw string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func (b *SpotifyBridge) acceptRequestSeq(kind string, seq int) bool {
	if seq <= 0 {
		return true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	switch kind {
	case "openCollection":
		if seq <= b.openCollectionReqSeq {
			return false
		}
		b.openCollectionReqSeq = seq
		return true
	case "streamTrack":
		if seq <= b.streamTrackReqSeq {
			return false
		}
		b.streamTrackReqSeq = seq
		return true
	case "startPlayback":
		if seq <= b.startPlaybackReqSeq {
			return false
		}
		b.startPlaybackReqSeq = seq
		return true
	case "playAdjacent":
		if seq <= b.playAdjacentReqSeq {
			return false
		}
		b.playAdjacentReqSeq = seq
		return true
	case "computePrebuffer":
		if seq <= b.computePrebufferReqSeq {
			return false
		}
		b.computePrebufferReqSeq = seq
		return true
	case "legacyNavOpen":
		if seq <= b.navOpenReqSeq {
			return false
		}
		b.navOpenReqSeq = seq
		return true
	case "navigateBackNonce":
		if seq <= b.navBackReqSeq {
			return false
		}
		b.navBackReqSeq = seq
		return true
	case "navBackNonce":
		if seq <= b.navBackReqSeq {
			return false
		}
		b.navBackReqSeq = seq
		return true
	case "loadMoreTracksNonce":
		if seq <= b.loadMoreTracksReqSeq {
			return false
		}
		b.loadMoreTracksReqSeq = seq
		return true
	case "prebufferTracksRequest":
		if seq <= b.prebufferTracksReqSeq {
			return false
		}
		b.prebufferTracksReqSeq = seq
		return true
	case "clearSessionNonce":
		if seq <= b.clearSessionReqSeq {
			return false
		}
		b.clearSessionReqSeq = seq
		return true
	case "computePrebufferNonce":
		if seq <= b.computePrebufferReqSeq {
			return false
		}
		b.computePrebufferReqSeq = seq
		return true
	case "uiIntent":
		if seq <= b.openCollectionReqSeq {
			return false
		}
		b.openCollectionReqSeq = seq
		return true
	default:
		return true
	}
}

func (b *SpotifyBridge) loadSessionState() {
	if b.sessionMgr == nil {
		return
	}
	s, err := b.sessionMgr.Load()
	if err != nil {
		b.set("trackListStatus", "Session load failed: "+err.Error())
		return
	}
	b.publishSessionState()

	contextURI := strings.TrimSpace(s.ContextURI)
	trackURI := strings.TrimSpace(s.TrackURI)
	if contextURI != "" && trackURI != "" {
		title := strings.TrimSpace(s.ContextName)
		if title == "" {
			title = "Collection"
		}

		b.mu.Lock()
		b.resumeActive = true
		b.resumeTargetTrackURI = trackURI
		if s.PositionMs > 0 {
			b.resumeTargetPosMs = s.PositionMs
		} else {
			b.resumeTargetPosMs = 0
		}
		b.mu.Unlock()

		fromViewMode := strings.TrimSpace(b.props.Value("viewMode").ToString())
		b.openCollectionContext(contextURI, title, fromViewMode)
	} else {
		b.clearResumePending()
	}

	b.mu.Lock()
	b.sessionLoadedSeq += 1
	nonce := b.sessionLoadedSeq
	b.mu.Unlock()
	b.set("sessionLoadedNonce", nonce)
}

func (b *SpotifyBridge) clearResumePending() {
	b.mu.Lock()
	b.resumeActive = false
	b.resumeTargetTrackURI = ""
	b.resumeTargetPosMs = 0
	b.mu.Unlock()
}

func (b *SpotifyBridge) saveSessionStateFromBridge() {
	if b.sessionMgr == nil {
		return
	}
	b.sessionMgr.Update(func(s *appapi.SessionState) {
		s.ContextURI = strings.TrimSpace(b.props.Value("sessionContextURI").ToString())
		s.ContextName = strings.TrimSpace(b.props.Value("sessionContextName").ToString())
		s.TrackURI = strings.TrimSpace(b.props.Value("sessionTrackURI").ToString())
		s.TrackName = strings.TrimSpace(b.props.Value("sessionTrackName").ToString())
		s.TrackArtist = strings.TrimSpace(b.props.Value("sessionTrackArtist").ToString())
		s.AlbumArtURL = strings.TrimSpace(b.props.Value("sessionAlbumArtURL").ToString())
		if pos, err := strconv.Atoi(strings.TrimSpace(b.props.Value("sessionPositionMs").ToString())); err == nil {
			s.PositionMs = pos
		}
		if vol, err := strconv.ParseFloat(strings.TrimSpace(b.props.Value("sessionUserVolume").ToString()), 64); err == nil {
			s.UserVolume = vol
		}
		s.DataSavingMode = strings.EqualFold(strings.TrimSpace(b.props.Value("sessionDataSavingMode").ToString()), "true")
	})
	if err := b.sessionMgr.Save(); err != nil {
		b.set("trackListStatus", "Session save failed: "+err.Error())
		return
	}
	b.publishSessionState()
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
		b.set("lastError", err.Error())
		b.set("statusText", "Spotify auth: failed to start")
		return
	}

	b.mu.Lock()
	b.flow = start.Flow
	b.mu.Unlock()

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
	b.set("statusText", "Spotify auth: checking cached credentials...")

	credentialsFile := bridgeCredentialsFile()

	hasStored, err := libspotdl.HasStoredCredentials(credentialsFile)
	if err != nil {
		b.set("isBusy", false)
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
		b.set("lastError", "")
		b.set("statusText", "Spotify auth: using cached credentials")
		go b.loadLibrary()
		return
	}

	if hasOAuth {
		b.set("isBusy", false)
		b.set("lastError", "")
		b.set("statusText", "Spotify auth: token cached, finish connection")
		return
	}

	b.set("isBusy", false)
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
			b.set("lastError", err.Error())
			b.set("statusText", "Spotify auth: session expired, reauth needed")
			b.set("libraryStatus", "Spotify library: re-authenticate to refresh access token")
			go b.startFlow()
			return
		}
		if strings.Contains(errText, "insufficient client scope") {
			b.set("lastError", err.Error())
			b.set("statusText", "Spotify auth: additional permissions required, reauth needed")
			b.set("libraryStatus", "Spotify library: re-authenticate to grant playlist/library scopes")
			go b.startFlow()
			return
		}
		if libspotdl.IsSpotifyCredentialRefusedError(err) {
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
	b.set("likedSongsName", snapshot.Liked.Name)
	b.set("likedSongsCount", snapshot.Liked.TrackCount)
	if snapshot.LikedAvailable {
		b.set("libraryStatus", fmt.Sprintf("Spotify library: loaded (%d playlists, %d liked songs)", len(snapshot.Playlists), snapshot.Liked.TrackCount))
		return
	}

	b.set("libraryStatus", fmt.Sprintf("Spotify library: playlists loaded (%d), liked songs unavailable", len(snapshot.Playlists)))
}

func (b *SpotifyBridge) openCollectionContext(contextURI, title, fromViewMode string) {

	if b.navMgr != nil {
		b.navMgr.OpenCollection(contextURI, title, fromViewMode)
		b.publishNavigationState()
	}

	b.set("isLoadingTracks", true)
	b.set("trackListTitle", title)
	b.set("trackListStatus", "Loading tracks...")
	b.set("trackListJson", "[]")
	b.set("trackHasMore", false)
	b.set("lastError", "")
	b.set("viewMode", "tracks")
	if b.trackModel != nil {
		b.trackModel.Reset()
		b.publishTrackModelState()
	}

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
	b.tryDispatchResumeSelection(allTracks, page.HasMore)
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

func (b *SpotifyBridge) tryDispatchResumeSelection(allTracks []libspotdl.LibraryTrackSummary, hasMore bool) {
	b.mu.Lock()
	resumeActive := b.resumeActive
	targetURI := strings.TrimSpace(b.resumeTargetTrackURI)
	targetPos := b.resumeTargetPosMs
	b.mu.Unlock()

	if !resumeActive || targetURI == "" {
		return
	}

	resumeIndex := -1
	for i := range allTracks {
		if strings.TrimSpace(allTracks[i].URI) == targetURI {
			resumeIndex = i
			break
		}
	}

	if resumeIndex >= 0 {
		track := allTracks[resumeIndex]
		intent := appapi.PlaybackIntent{
			Index:          resumeIndex,
			ShouldPlay:     false,
			Name:           strings.TrimSpace(track.Name),
			URI:            strings.TrimSpace(track.URI),
			ArtistText:     strings.TrimSpace(track.ArtistText),
			AlbumArtURL:    strings.TrimSpace(track.AlbumArtURL),
			DownloadedPath: strings.TrimSpace(track.DownloadedPath),
			DurationMs:     int(track.DurationMs),
		}
		b.publishAction("playback_select", "session_resume", "", targetPos, 0, intent)
		b.clearResumePending()
		return
	}

	if !hasMore {
		b.clearResumePending()
		return
	}

	go b.loadNextTrackPage()
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
	b.clearPrebuffer()

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

func (b *SpotifyBridge) clearPrebuffer() {
	b.mu.Lock()
	if b.prebufferCancel != nil {
		b.prebufferCancel()
		b.prebufferCancel = nil
	}
	b.prebufferTrackURI = ""
	b.prebufferOpID++
	b.mu.Unlock()
}

func (b *SpotifyBridge) prebufferTracks(raw string) {
	parts := strings.Split(raw, "\n")
	uris := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trackURI := strings.TrimSpace(part)
		if !strings.HasPrefix(trackURI, "spotify:track:") {
			continue
		}
		if _, ok := seen[trackURI]; ok {
			continue
		}
		seen[trackURI] = struct{}{}
		uris = append(uris, trackURI)
	}
	if len(uris) == 0 {
		return
	}

	cacheDir := streamCacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	b.pruneStreamCacheIfLowDisk(cacheDir)

	b.mu.Lock()
	if b.prebufferCancel != nil {
		b.prebufferCancel()
		b.prebufferCancel = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	b.prebufferCancel = cancel
	b.prebufferTrackURI = uris[0]
	b.prebufferOpID++
	opID := b.prebufferOpID
	b.mu.Unlock()

	defer func() {
		cancel()
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
		return
	}
	defer func() {
		_ = loader.Close()
	}()

	for _, trackURI := range uris {
		if ctx.Err() != nil {
			return
		}
		b.mu.Lock()
		isCurrent := b.prebufferOpID == opID
		b.mu.Unlock()
		if !isCurrent {
			return
		}
		b.prebufferSingleTrack(ctx, loader, trackURI)
	}
}

func (b *SpotifyBridge) prebufferSingleTrack(ctx context.Context, loader *libspotdl.Downloader, trackURI string) {
	outputPath := streamCachePathForURI(trackURI)
	if isUsableCachedStream(outputPath) {
		_ = os.Chtimes(outputPath, time.Now(), time.Now())
		return
	}

	tmpPath := outputPath + ".prefetch.part"
	_ = os.Remove(tmpPath)
	_ = os.Remove(streamCacheDoneMarkerPathForStream(outputPath))

	f, err := os.Create(tmpPath)
	if err != nil {
		return
	}
	defer func() {
		_ = f.Close()
	}()

	_, _, err = loader.StreamTrack(ctx, trackURI, []io.Writer{f}, nil)
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
	if len(parts) >= 3 {
		if seq, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
			if !b.acceptRequestSeq("streamTrack", seq) {
				return
			}
		}
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
	if b.trackModel != nil {
		b.trackModel.UpdateFromSummaries(items)
		b.publishTrackModelState()
	}
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
