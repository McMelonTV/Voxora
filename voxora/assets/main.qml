import QtQuick
import QtQuick.Controls
import QtMultimedia
import QtQuick.Shapes
import QtQuick.Window
import Qt.labs.settings

Window {
    width: 576
    height: 1024
    visible: true

    title: "Voxora"
    color: "#0f1115"
    property string currentPlayingPath: ""
    property int lastStreamReadyNonce: 0
    property bool manualStopRequested: false
    property bool streamRecovering: false
    property bool scrubbingActive: false
    property bool resumeAfterScrub: false
    property int streamRecoverAttempts: 0
    property int streamRecoverTargetPositionMs: -1
    property int streamOpenBufferedBytes: 0
    property bool streamSourceSwitching: false
    property bool streamSourceSwitchShouldPlay: false
    property int lastPartRefreshAtMs: -1000000
    property int expectedDurationMs: 0
    property int effectiveDurationMs: Math.max(localPlayer.duration, expectedDurationMs)
    property int pendingResumePositionMs: -1
    property int pendingSeekTargetMs: -1
    property real pendingSeekRatio: -1
    property int pendingResumeAttempts: 0
    property int loadMoreRequestedForCount: -1
    property real userVolume: 0.8
    property var navigationStack: []
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property string currentTrackTitle: ""
    property string currentTrackArtist: ""
    property string currentTrackAlbumArtUrl: ""
    property string currentTrackURI: ""
    property string currentTrackDownloadedPath: ""
    property int currentTrackIndex: -1
    property bool autoplayEnabled: true
    property bool streamShouldAutoPlay: true
    property bool holdStoppedTrackState: false
    property string activeContextURI: ""
    property string activeContextName: ""
    property bool restoringSession: false
    property string resumeTargetTrackURI: ""
    property int resumeTargetPositionMs: -1
    property real playbackProgress: effectiveDurationMs > 0 ? Math.max(0, Math.min(1, localPlayer.position / effectiveDurationMs)) : 0
    property bool usingStreamSource: currentPlayingPath.length > 0 && currentPlayingPath === (spotifyAuthBridge.streamPlayPath || "")
    property bool streamFullyBuffered: {
        if (!usingStreamSource) {
            return true
        }
        var total = Number(spotifyAuthBridge.streamBufferedTotal)
        var have = Number(spotifyAuthBridge.streamBufferedBytes)
        if (!isNaN(total) && !isNaN(have) && total > 0) {
            return have >= Math.max(0, total - 32768)
        }
        return !spotifyAuthBridge.isStreamingTrack
    }
    property bool seekEnabledForCurrentSource: !usingStreamSource || streamFullyBuffered
    property real bufferedProgressLatched: 0
    property real streamFetchProgress: {
        var total = Number(spotifyAuthBridge.streamBufferedTotal)
        var have = Number(spotifyAuthBridge.streamBufferedBytes)
        if (!isNaN(total) && !isNaN(have) && total > 0) {
            return Math.max(0, Math.min(1, have / total))
        }
        return -1
    }
    property real bufferedProgress: {
        var raw = 0
        if (usingStreamSource) {
            raw = Math.max(bufferedProgressLatched, streamFetchProgress)
        } else {
            raw = Math.max(bufferedProgressLatched, Number(localPlayer.bufferProgress))
        }
        if (isNaN(raw)) {
            raw = 0
        }
        raw = Math.max(0, Math.min(1, raw))
        return Math.max(playbackProgress, raw)
    }

    Keys.onReleased: function(event) {
        if ((event.key === Qt.Key_Back || event.key === Qt.Key_Escape) && handleBackNavigation()) {
            event.accepted = true
        }
    }

    onClosing: function(close) {
        if (close.accepted) {
            persistSessionState()
        }
        if (handleBackNavigation()) {
            close.accepted = false
        }
    }

    Settings {
        id: sessionState
        category: "playback_session"
        property string contextURI: ""
        property string contextName: ""
        property string trackURI: ""
        property string trackName: ""
        property string trackArtist: ""
        property string albumArtURL: ""
        property int positionMs: 0
    }

    Shortcut {
        sequence: "Esc"
        context: Qt.ApplicationShortcut
        onActivated: handleBackNavigation()
    }

    Shortcut {
        sequence: "Back"
        context: Qt.ApplicationShortcut
        onActivated: handleBackNavigation()
    }

    onStreamFetchProgressChanged: {
        if (streamFetchProgress >= 0) {
            bufferedProgressLatched = Math.max(bufferedProgressLatched, streamFetchProgress)
        }
    }

    Connections {
        target: localPlayer
        function onBufferProgressChanged() {
            var raw = Number(localPlayer.bufferProgress)
            if (!isNaN(raw)) {
                raw = Math.max(0, Math.min(1, raw))
                bufferedProgressLatched = Math.max(bufferedProgressLatched, raw)
            }
        }
    }

    function toFileUrl(path) {
        if (!path) {
            return ""
        }
        if (path.startsWith("file://") || path.startsWith("http://") || path.startsWith("https://")) {
            return path
        }
        return "file://" + path
    }

    function formatMs(ms) {
        var totalSec = Math.max(0, Math.floor((ms || 0) / 1000))
        var m = Math.floor(totalSec / 60)
        var s = totalSec % 60
        return m + ":" + (s < 10 ? "0" + s : s)
    }

    function isPartStreamPath(path) {
        if (!path) {
            return false
        }
        return path.endsWith(".stream.play.ogg") || path.endsWith(".stream.ogg.part") || (path.indexOf("/stream?kind=play&") !== -1)
    }

    function recoverStreamingPlayback() {
        if (manualStopRequested || streamRecovering) {
            return
        }

        var targetPath = currentPlayingPath || ""
        if (targetPath.length === 0) {
            return
        }

        streamRecovering = true
        streamRecoverAttempts += 1
        streamRecoverTargetPositionMs = Math.max(0, localPlayer.position)

        // Never reopen source here; keep continuous reader handle to avoid audible gaps.
        var currentSource = localPlayer.source ? localPlayer.source.toString() : ""
        var targetSource = toFileUrl(targetPath)
        var isLiveFifo = targetPath.endsWith(".live.fifo")
        var cachePath = spotifyAuthBridge.streamCacheFilePath || spotifyAuthBridge.streamCachePath || ""
        var usingCachePath = cachePath.length > 0 && targetPath === cachePath
        if (currentSource.length === 0 && targetSource.length > 0) {
            localPlayer.source = targetSource
        }

        if (!isLiveFifo && streamRecoverTargetPositionMs >= 0) {
            pendingResumePositionMs = streamRecoverTargetPositionMs
            pendingSeekRatio = -1
            pendingSeekTargetMs = -1
            pendingResumeAttempts = 0

            // When Android MediaPlayer stops after hitting a temporary EOF on a growing file,
            // force a source reload so it re-reads updated file length and can continue.
            if (usingCachePath && targetSource.length > 0) {
                streamSourceSwitching = true
                streamSourceSwitchShouldPlay = true
                localPlayer.source = ""
                localPlayer.source = targetSource
            }
        }
        recoverStreamingTimer.restart()
    }

    function refreshPartStreamSource(targetPosMs) {
        if (streamSourceSwitching) {
            return
        }
        var targetPath = currentPlayingPath || ""
        if (!isPartStreamPath(targetPath)) {
            return
        }
        var nowMs = Date.now()
        if ((nowMs - lastPartRefreshAtMs) < 1200) {
            return
        }
        lastPartRefreshAtMs = nowMs

        var wasPlaying = localPlayer.playbackState === MediaPlayer.PlayingState
        streamSourceSwitching = true
        streamSourceSwitchShouldPlay = wasPlaying
        pendingResumePositionMs = Math.max(0, targetPosMs)
        pendingSeekRatio = -1
        pendingSeekTargetMs = -1
        pendingResumeAttempts = 0
        localPlayer.source = ""
        localPlayer.source = toFileUrl(targetPath)
    }

    function seekToX(mouseX, trackWidth) {
        if (trackWidth <= 0) {
            return
        }
        var ratio = mouseX / trackWidth
        ratio = Math.max(0, Math.min(1, ratio))
        seekToRatio(ratio)
    }

    function seekToRatio(ratio) {
        if (effectiveDurationMs <= 0) {
            return
        }

        var cachePath = spotifyAuthBridge.streamCacheFilePath || spotifyAuthBridge.streamCachePath || ""
        var isLiveFifo = currentPlayingPath.endsWith(".live.fifo")
        var isPartStream = isPartStreamPath(currentPlayingPath)

        if (!seekEnabledForCurrentSource) {
            if ((isLiveFifo || isPartStream) && spotifyAuthBridge.streamCacheReady && cachePath.length > 0) {
                streamSourceSwitching = true
                streamSourceSwitchShouldPlay = localPlayer.playbackState === MediaPlayer.PlayingState
                pendingSeekRatio = Math.max(0, Math.min(1, ratio))
                pendingResumePositionMs = Math.floor(effectiveDurationMs * pendingSeekRatio)
                pendingResumeAttempts = 0
                currentPlayingPath = cachePath
                localPlayer.source = ""
                localPlayer.source = toFileUrl(cachePath)
                return
            }
            spotifyAuthBridge.trackListStatus = "Seek is available after stream buffering finishes"
            return
        }

        ratio = Math.max(0, Math.min(1, ratio))
        var targetPos = Math.floor(effectiveDurationMs * ratio)

        if (isLiveFifo || isPartStream) {
            var cachedPath = cachePath
            var have = Number(spotifyAuthBridge.streamBufferedBytes)
            var total = Number(spotifyAuthBridge.streamBufferedTotal)
            var canSeekInCache = spotifyAuthBridge.streamCacheReady && cachedPath.length > 0
            if (canSeekInCache && !isNaN(have) && !isNaN(total) && total > 0) {
                var bufferedRatio = Math.max(0, Math.min(1, have / total))
                canSeekInCache = ratio <= Math.max(0, bufferedRatio - 0.01)
            }
            if (canSeekInCache) {
                streamSourceSwitching = true
                streamSourceSwitchShouldPlay = localPlayer.playbackState === MediaPlayer.PlayingState
                pendingSeekRatio = ratio
                pendingResumePositionMs = targetPos
                pendingResumeAttempts = 0
                currentPlayingPath = cachedPath
                localPlayer.source = toFileUrl(cachedPath)
            } else {
                spotifyAuthBridge.trackListStatus = "Seek target not buffered yet"
            }
            return
        }

        localPlayer.position = targetPos
        pendingResumePositionMs = -1
        pendingSeekTargetMs = -1
        pendingSeekRatio = -1
        pendingResumeAttempts = 0
        resumeSeekTimer.stop()
    }

    function togglePlayPause() {
        if (!localPlayer.source || localPlayer.source.toString().length === 0) {
            return
        }
        holdStoppedTrackState = false
        if (localPlayer.playbackState === MediaPlayer.PlayingState) {
            localPlayer.pause()
        } else {
            localPlayer.play()
        }
    }

    function startTrackAtIndex(index, shouldPlay) {
        var count = trackListModel.count
        if (index < 0 || index >= count) {
            return
        }

        var item = trackListModel.get(index)
        if (!item) {
            return
        }

        var trackUri = item.URI || ""
        var trackName = item.Name || "Track"
        var artistText = item.ArtistText || ""
        var downloadedPath = item.DownloadedPath || ""
        var isDownloaded = downloadedPath.length > 0
        var durationMs = item.DurationMs || 0

        currentTrackIndex = index
        currentTrackURI = trackUri
        currentTrackTitle = trackName || trackUri || "Unknown track"
        currentTrackArtist = artistText
        currentTrackAlbumArtUrl = item.AlbumArtURL || ""
        currentTrackDownloadedPath = downloadedPath
        persistSessionState()

        holdStoppedTrackState = !shouldPlay
        streamShouldAutoPlay = shouldPlay
        manualStopRequested = true
        streamRecovering = false
        streamRecoverAttempts = 0
        streamRecoverTargetPositionMs = -1
        pendingResumePositionMs = -1
        pendingSeekTargetMs = -1
        pendingSeekRatio = -1
        pendingResumeAttempts = 0
        resumeSeekTimer.stop()
        localPlayer.stop()
        spotifyAuthBridge.clearStreamNonce = Date.now()
        manualStopRequested = false

        expectedDurationMs = durationMs
        if (isDownloaded) {
            currentPlayingPath = downloadedPath
            localPlayer.source = toFileUrl(currentPlayingPath)
            if (shouldPlay) {
                localPlayer.play()
            }
            requestNextTrackPrebuffer()
            return
        }

        spotifyAuthBridge.streamTrackRequest = trackUri + "\n" + trackName + "\n" + Date.now()
        requestNextTrackPrebuffer()
    }

    function requestNextTrackPrebuffer() {
        if (!autoplayEnabled || trackListModel.count <= 0 || currentTrackIndex < 0) {
            return
        }
        var nextIndex = currentTrackIndex + 1
        if (nextIndex < 0 || nextIndex >= trackListModel.count) {
            return
        }
        var nextItem = trackListModel.get(nextIndex)
        if (!nextItem) {
            return
        }
        var downloadedPath = nextItem.DownloadedPath || ""
        if (downloadedPath.length > 0) {
            return
        }
        var nextURI = nextItem.URI || ""
        if (nextURI.length === 0) {
            return
        }
        spotifyAuthBridge.prebufferTrackRequest = nextURI + "\n" + (nextItem.Name || "Track") + "\n" + Date.now()
    }

    function playTrackAtIndex(index) {
        startTrackAtIndex(index, true)
    }

    function playAdjacentTrack(step) {
        if (trackListModel.count <= 0) {
            return
        }
        var start = currentTrackIndex
        if (start < 0) {
            start = 0
        }
        var next = start + step
        if (next < 0 || next >= trackListModel.count) {
            return
        }
        playTrackAtIndex(next)
    }

    function pushNavigationEntry(entry) {
        var next = navigationStack.slice(0)
        next.push(entry)
        navigationStack = next
    }

    function openCollectionFromUI(uri, name) {
        var cleanUri = (uri || "").trim()
        if (cleanUri.length === 0) {
            return
        }
        activeContextURI = cleanUri
        activeContextName = name || "Collection"
        pushNavigationEntry({ fromViewMode: spotifyAuthBridge.viewMode })
        spotifyAuthBridge.openCollectionRequest = cleanUri + "\n" + (name || "Collection") + "\n" + Date.now()
    }

    function persistSessionState() {
        if (activeContextURI.length === 0 || currentTrackURI.length === 0) {
            return
        }
        sessionState.contextURI = activeContextURI
        sessionState.contextName = activeContextName
        sessionState.trackURI = currentTrackURI
        sessionState.trackName = currentTrackTitle
        sessionState.trackArtist = currentTrackArtist
        sessionState.albumArtURL = currentTrackAlbumArtUrl
        sessionState.positionMs = Math.max(0, Number(localPlayer.position) || 0)
    }

    function restoreSessionState() {
        var contextURI = (sessionState.contextURI || "").trim()
        var trackURI = (sessionState.trackURI || "").trim()
        if (contextURI.length === 0 || trackURI.length === 0) {
            return
        }

        activeContextURI = contextURI
        activeContextName = (sessionState.contextName || "Collection").trim()
        resumeTargetTrackURI = trackURI
        resumeTargetPositionMs = Math.max(0, Number(sessionState.positionMs) || 0)
        currentTrackURI = trackURI
        currentTrackTitle = sessionState.trackName || ""
        currentTrackArtist = sessionState.trackArtist || ""
        currentTrackAlbumArtUrl = sessionState.albumArtURL || ""
        restoringSession = true

        spotifyAuthBridge.openCollectionRequest = activeContextURI + "\n" + (activeContextName || "Collection") + "\n" + Date.now()
    }

    function handleBackNavigation() {
        if (spotifyAuthBridge.viewMode === "tracks") {
            if (navigationStack.length > 0) {
                navigationStack = navigationStack.slice(0, navigationStack.length - 1)
            }
            spotifyAuthBridge.navigateBackNonce = Date.now()
            return true
        }
        return false
    }

    function activeTrackListView() {
        if (spotifyAuthBridge.viewMode !== "tracks") {
            return null
        }
        if (!contentLoader.item || !contentLoader.item.trackListViewRef) {
            return null
        }
        return contentLoader.item.trackListViewRef
    }

    function requestLoadMoreTracksPreserveScroll(forceRequest) {
        var force = !!forceRequest
        if (spotifyAuthBridge.isLoadingTracks || !spotifyAuthBridge.trackHasMore) {
            return
        }
        var currentCount = trackListModel.count
        if (!force && currentCount === loadMoreRequestedForCount) {
            return
        }
        if (!force) {
            loadMoreRequestedForCount = currentCount
        }
        spotifyAuthBridge.loadMoreTracksNonce = Date.now()
    }

    function applyPendingResume() {
        if (pendingResumePositionMs < 0 && pendingSeekRatio < 0) {
            return
        }
        var baseDuration = effectiveDurationMs
        var target = pendingResumePositionMs
        if (pendingSeekRatio >= 0 && baseDuration > 0) {
            target = Math.floor(baseDuration * pendingSeekRatio)
        }
        var maxPos = baseDuration > 1000 ? (baseDuration - 250) : target
        target = Math.max(0, Math.min(target, maxPos))

        var readyForSeek = seekEnabledForCurrentSource && (localPlayer.seekable || (localPlayer.duration > 0 && !currentPlayingPath.endsWith(".live.fifo")))
        if (readyForSeek) {
            localPlayer.position = target
            pendingSeekTargetMs = target
        } else if (holdStoppedTrackState) {
            // Restored non-playing sessions can become seekable later than normal.
            return
        }

        pendingResumeAttempts += 1
        var maxAttempts = holdStoppedTrackState ? 600 : 60
        if (pendingResumeAttempts > maxAttempts) {
            pendingResumePositionMs = -1
            pendingSeekTargetMs = -1
            pendingSeekRatio = -1
            pendingResumeAttempts = 0
            resumeSeekTimer.stop()
        }
    }

    AudioOutput {
        id: localAudioOutput
        volume: userVolume

        Behavior on volume {
            NumberAnimation {
                duration: 90
                easing.type: Easing.InOutQuad
            }
        }
    }

    MediaPlayer {
        id: localPlayer
        audioOutput: localAudioOutput
        onMediaStatusChanged: {
            if (mediaStatus === MediaPlayer.EndOfMedia && autoplayEnabled && !manualStopRequested && !streamSourceSwitching) {
                playAdjacentTrack(1)
                return
            }
            if (holdStoppedTrackState && (mediaStatus === MediaPlayer.LoadedMedia || mediaStatus === MediaPlayer.BufferedMedia || mediaStatus === MediaPlayer.BufferingMedia) && pendingResumePositionMs >= 0) {
                localPlayer.position = Math.max(0, pendingResumePositionMs)
                pendingSeekTargetMs = Math.max(0, pendingResumePositionMs)
                if (localPlayer.playbackState === MediaPlayer.StoppedState) {
                    localPlayer.pause()
                }
            }
            if (streamSourceSwitching && (mediaStatus === MediaPlayer.LoadedMedia || mediaStatus === MediaPlayer.BufferedMedia || mediaStatus === MediaPlayer.BufferingMedia)) {
                var target = Math.max(0, pendingResumePositionMs)
                localPlayer.position = target
                pendingSeekTargetMs = target
                pendingResumeAttempts = 0
                if (streamSourceSwitchShouldPlay) {
                    localPlayer.play()
                }
                streamSourceSwitching = false
                streamSourceSwitchShouldPlay = false
                resumeSeekTimer.restart()
            }
            applyPendingResume()
        }
        onPositionChanged: {
            if (!streamSourceSwitching && !scrubbingActive && isPartStreamPath(currentPlayingPath)) {
                var total = Number(spotifyAuthBridge.streamBufferedTotal)
                var have = Number(spotifyAuthBridge.streamBufferedBytes)
                if (!!spotifyAuthBridge.isStreamingTrack && !isNaN(total) && !isNaN(have) && total > 0 && effectiveDurationMs > 0) {
                    var bufferedRatio = Math.max(0, Math.min(1, have / total))
                    var bufferedMs = Math.floor(effectiveDurationMs * bufferedRatio)
                    if (bufferedMs > 0 && localPlayer.position >= Math.max(0, bufferedMs - 2500)) {
                        refreshPartStreamSource(localPlayer.position)
                    }
                }
            }

            if (pendingSeekTargetMs >= 0) {
                if (Math.abs(localPlayer.position - pendingSeekTargetMs) <= 1500) {
                    pendingResumePositionMs = -1
                    pendingSeekTargetMs = -1
                    pendingSeekRatio = -1
                    pendingResumeAttempts = 0
                    resumeSeekTimer.stop()
                }
            }
        }
        onPlaybackStateChanged: {
            if (playbackState === MediaPlayer.PlayingState) {
                holdStoppedTrackState = false
                streamRecovering = false
                streamRecoverAttempts = 0
                streamRecoverTargetPositionMs = -1
                requestNextTrackPrebuffer()
            }

            if (playbackState === MediaPlayer.StoppedState) {
                if (holdStoppedTrackState) {
                    return
                }
                if (streamSourceSwitching) {
                    return
                }
                var cachePath = spotifyAuthBridge.streamCacheFilePath || spotifyAuthBridge.streamCachePath || ""
                var bufferedNow = Number(spotifyAuthBridge.streamBufferedBytes) || 0
                var likelyNaturalEnd = effectiveDurationMs > 0 && localPlayer.position >= (effectiveDurationMs - 1500)
                var isPotentialStreamStop = currentPlayingPath.length > 0 && (
                    currentPlayingPath.endsWith(".live.fifo")
                    || (cachePath.length > 0 && currentPlayingPath === cachePath)
                    || (spotifyAuthBridge.streamPlayPath || "") === currentPlayingPath
                )
                var hasNewBufferedData = bufferedNow > (streamOpenBufferedBytes + 8192)
                var stillDownloading = !!spotifyAuthBridge.isStreamingTrack
                var isCacheStop = cachePath.length > 0 && currentPlayingPath === cachePath
                var recoverableStop = !likelyNaturalEnd && (currentPlayingPath.endsWith(".live.fifo") || hasNewBufferedData || stillDownloading || isCacheStop)

                if (!manualStopRequested && !streamRecovering && isPotentialStreamStop && recoverableStop && streamRecoverAttempts < 6) {
                    recoverStreamingPlayback()
                    return
                }

                currentPlayingPath = ""
                manualStopRequested = false
                streamRecovering = false
                streamRecoverAttempts = 0
                streamRecoverTargetPositionMs = -1
                streamSourceSwitching = false
                streamSourceSwitchShouldPlay = false
            }
        }
    }

    Timer {
        id: resumeSeekTimer
        interval: 120
        repeat: true
        running: false
        onTriggered: applyPendingResume()
    }

    Timer {
        id: recoverStreamingTimer
        interval: 180
        repeat: false
        onTriggered: {
            if (!streamRecovering || manualStopRequested) {
                streamRecovering = false
                return
            }
            localPlayer.play()
            if (!currentPlayingPath.endsWith(".live.fifo") && streamRecoverTargetPositionMs >= 0) {
                pendingResumePositionMs = streamRecoverTargetPositionMs
                pendingSeekRatio = -1
                pendingSeekTargetMs = -1
                pendingResumeAttempts = 0
                resumeSeekTimer.restart()
            }
        }
    }

    Timer {
        interval: 200
        running: true
        repeat: true
        onTriggered: {
            if (spotifyAuthBridge.streamPlayReadyNonce > lastStreamReadyNonce) {
                lastStreamReadyNonce = spotifyAuthBridge.streamPlayReadyNonce
                var nextPath = spotifyAuthBridge.streamPlayPath || ""
                currentPlayingPath = nextPath
                if (nextPath.length > 0) {
                    manualStopRequested = false
                    streamRecovering = false
                    streamRecoverAttempts = 0
                    streamRecoverTargetPositionMs = -1
                    pendingResumePositionMs = -1
                    pendingSeekTargetMs = -1
                    pendingSeekRatio = -1
                    pendingResumeAttempts = 0
                    resumeSeekTimer.stop()
                    lastPartRefreshAtMs = -1000000
                    streamOpenBufferedBytes = Number(spotifyAuthBridge.streamBufferedBytes) || 0
                    bufferedProgressLatched = 0
                    localPlayer.source = ""
                    localPlayer.source = toFileUrl(nextPath)
                    if (streamShouldAutoPlay) {
                        localPlayer.play()
                    }
                }
            }
        }
    }

    property var playlistsData: {
        try {
            return JSON.parse(spotifyAuthBridge.playlistsJson)
        } catch (e) {
            return []
        }
    }

    property var libraryListEntries: {
        var base = []
        base.push({
            Name: spotifyAuthBridge.likedSongsName || "Liked Songs",
            URI: "spotify:collection:tracks",
            TrackCount: Number(spotifyAuthBridge.likedSongsCount) || 0,
            OwnerName: "Your Library"
        })
        for (var i = 0; i < playlistsData.length; i += 1) {
            base.push(playlistsData[i])
        }
        return base
    }

    property var trackListData: {
        try {
            return JSON.parse(spotifyAuthBridge.trackListJson)
        } catch (e) {
            return []
        }
    }

    ListModel {
        id: trackListModel
    }

    function normalizeTrackItem(item) {
        return {
            Name: item && item.Name ? item.Name : "",
            URI: item && item.URI ? item.URI : "",
            ArtistText: item && item.ArtistText ? item.ArtistText : "",
            AlbumArtURL: item && item.AlbumArtURL ? item.AlbumArtURL : "",
            DownloadedPath: item && item.DownloadedPath ? item.DownloadedPath : "",
            DurationMs: item && item.DurationMs ? Number(item.DurationMs) : 0
        }
    }

    function syncTrackListModel(items) {
        var incoming = Array.isArray(items) ? items : []
        var existingCount = trackListModel.count

        if (incoming.length === 0) {
            if (existingCount > 0) {
                trackListModel.clear()
            }
            return
        }

        var canIncremental = existingCount > 0 && incoming.length >= existingCount
        if (canIncremental) {
            for (var i = 0; i < existingCount; i += 1) {
                var existingItem = trackListModel.get(i)
                var nextItem = normalizeTrackItem(incoming[i])
                if ((existingItem.URI || "") !== (nextItem.URI || "") || (existingItem.Name || "") !== (nextItem.Name || "")) {
                    canIncremental = false
                    break
                }
            }
        }

        if (!canIncremental) {
            trackListModel.clear()
            for (var j = 0; j < incoming.length; j += 1) {
                trackListModel.append(normalizeTrackItem(incoming[j]))
            }
            return
        }

        for (var k = 0; k < existingCount; k += 1) {
            trackListModel.set(k, normalizeTrackItem(incoming[k]))
        }
        for (var n = existingCount; n < incoming.length; n += 1) {
            trackListModel.append(normalizeTrackItem(incoming[n]))
        }
    }

    onTrackListDataChanged: {
        syncTrackListModel(trackListData)

        if (restoringSession && resumeTargetTrackURI.length > 0 && trackListModel.count > 0) {
            var resumeIndex = -1
            for (var i = 0; i < trackListModel.count; i += 1) {
                var item = trackListModel.get(i)
                if (item && (item.URI || "") === resumeTargetTrackURI) {
                    resumeIndex = i
                    break
                }
            }

            if (resumeIndex >= 0) {
                startTrackAtIndex(resumeIndex, false)
                pendingResumePositionMs = resumeTargetPositionMs
                pendingSeekRatio = -1
                pendingSeekTargetMs = -1
                pendingResumeAttempts = 0
                resumeSeekTimer.restart()
                restoringSession = false
                resumeTargetTrackURI = ""
                resumeTargetPositionMs = -1
            } else if (spotifyAuthBridge.trackHasMore && !spotifyAuthBridge.isLoadingTracks) {
                requestLoadMoreTracksPreserveScroll(true)
            }
        }

        if (trackListModel.count === 0) {
            currentTrackIndex = -1
        } else if (currentTrackIndex >= trackListModel.count) {
            currentTrackIndex = trackListModel.count - 1
        }
        if (trackListData.length === 0) {
            loadMoreRequestedForCount = -1
            var lv = activeTrackListView()
            if (lv) {
                lv.autoLoadIssuedForCount = -1
                lv.autoLoadAwaitUserScroll = false
            }
        }
    }

    Timer {
        id: sessionPersistTimer
        interval: 1200
        repeat: true
        running: true
        onTriggered: {
            if (localPlayer.playbackState === MediaPlayer.PlayingState) {
                persistSessionState()
            }
        }
    }

    Component.onCompleted: {
        restoreSessionState()
    }

    Loader {
        id: contentLoader
        anchors.fill: parent
        sourceComponent: spotifyAuthBridge.viewMode === "tracks" ? trackListComponent : libraryListComponent
    }

    Component {
        id: libraryListComponent

        Column {
            anchors.fill: parent
            spacing: 0

            Rectangle {
                width: parent.width
                color: "#161616"
                border.color: "#2b2b2b"
                border.width: 1
                implicitHeight: controlsColumn.implicitHeight + 20

                Column {
                    id: controlsColumn
                    anchors.fill: parent
                    anchors.margins: 10
                    spacing: 6

                    Row {
                        spacing: 8

                        Button {
                            text: spotifyAuthBridge.isBusy ? "Authenticating..." : "Connect Spotify"
                            enabled: !spotifyAuthBridge.isBusy
                            onClicked: spotifyAuthBridge.startAuthNonce = Date.now()
                        }

                        Text {
                            text: spotifyAuthBridge.statusText
                            color: "#9ad0ff"
                            wrapMode: Text.WrapAnywhere
                            width: Math.max(120, controlsColumn.width - 170)
                        }
                    }

                    Text {
                        text: spotifyAuthBridge.libraryStatus
                        color: "#9ad0ff"
                        wrapMode: Text.WrapAnywhere
                    }

                    Row {
                        spacing: 10

                        Text {
                            text: "Volume"
                            color: "#8ea4c2"
                            verticalAlignment: Text.AlignVCenter
                        }

                        Slider {
                            width: 160
                            from: 0
                            to: 1
                            value: userVolume
                            onValueChanged: userVolume = value
                        }

                        Button {
                            text: "Clear Stream Cache"
                            onClicked: spotifyAuthBridge.clearStreamCacheAllNonce = Date.now()
                        }
                    }
                }
            }

            ListView {
                width: parent.width
                height: Math.max(0, parent.height - (controlsColumn.implicitHeight + 20))
                clip: true
                model: libraryListEntries
                delegate: Rectangle {
                    width: ListView.view ? ListView.view.width : parent.width
                    height: 56
                    color: index % 2 === 0 ? "#111722" : "#0f141d"

                    Row {
                        anchors.fill: parent
                        anchors.margins: 10
                        spacing: 10

                        Text {
                            text: (modelData.Name || "Unnamed playlist") + " (" + (modelData.TrackCount || 0) + ")"
                            color: "#e8edf5"
                            width: Math.max(120, parent.width - 120)
                            elide: Text.ElideRight
                        }

                        Text {
                            text: modelData.OwnerName ? "by " + modelData.OwnerName : ""
                            color: "#93a1b5"
                            width: 90
                            elide: Text.ElideRight
                        }
                    }

                    MouseArea {
                        anchors.fill: parent
                        onClicked: openCollectionFromUI(modelData.URI || "", modelData.Name || "Playlist")
                    }
                }
            }
        }
    }

    Component {
        id: trackListComponent

        Column {
            id: trackListRoot
            property alias trackListViewRef: trackListView
            anchors.fill: parent
            spacing: 0

            Rectangle {
                id: trackHeader
                width: parent.width
                height: 52
                color: "#121c2d"
                border.color: "#2d3f5f"
                border.width: 1

                Row {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 10

                    Button {
                        width: 44
                        height: 36
                        onClicked: handleBackNavigation()

                        contentItem: Item {
                            anchors.fill: parent

                            Shape {
                                anchors.centerIn: parent
                                width: 14
                                height: 14
                                antialiasing: true

                                ShapePath {
                                    strokeColor: actionButtonTextColor
                                    strokeWidth: 2
                                    fillColor: "transparent"
                                    capStyle: ShapePath.RoundCap
                                    joinStyle: ShapePath.RoundJoin
                                    startX: 10
                                    startY: 2
                                    PathLine { x: 4; y: 7 }
                                    PathLine { x: 10; y: 12 }
                                }
                            }
                        }
                    }

                    Text {
                        text: spotifyAuthBridge.trackListTitle
                        color: "#e8edf5"
                        width: Math.max(120, parent.width - 120)
                        elide: Text.ElideRight
                    }
                }
            }

            Text {
                id: trackStatusText
                width: parent.width
                text: spotifyAuthBridge.trackListStatus
                color: "#9ad0ff"
                wrapMode: Text.WrapAnywhere
                padding: 8
            }

            ListView {
                id: trackListView
                width: parent.width
                height: Math.max(120, parent.height - trackHeader.height - trackStatusText.implicitHeight - nowPlayingPanel.height - playbackPanel.height)
                clip: true
                reuseItems: true
                cacheBuffer: 0
                displayMarginBeginning: 0
                displayMarginEnd: 0
                model: trackListModel
                property int autoLoadIssuedForCount: -1
                property bool autoLoadAwaitUserScroll: false

                function requestMoreIfNeeded() {
                    var currentCount = trackListModel.count
                    if (currentCount <= 0) {
                        autoLoadIssuedForCount = -1
                        autoLoadAwaitUserScroll = false
                        return
                    }
                    var prefetchRows = 10
                    var estimatedRowHeight = 52
                    var prefetchDistance = prefetchRows * estimatedRowHeight
                    var nearEnd = (contentY + height) >= (contentHeight - prefetchDistance)
                    if (!autoLoadAwaitUserScroll && currentCount !== autoLoadIssuedForCount && !spotifyAuthBridge.isLoadingTracks && spotifyAuthBridge.trackHasMore && nearEnd) {
                        autoLoadIssuedForCount = currentCount
                        autoLoadAwaitUserScroll = true
                        requestLoadMoreTracksPreserveScroll(false)
                    }
                }

                onMovementStarted: {
                    autoLoadAwaitUserScroll = false
                }

                onMovementEnded: requestMoreIfNeeded()

                delegate: Rectangle {
                    width: ListView.view ? ListView.view.width : parent.width
                    height: 60
                    color: index % 2 === 0 ? "#0f1623" : "#0d131d"
                    property bool isDownloaded: !!(DownloadedPath && String(DownloadedPath).trim().length > 0)
                    property int actionButtonHeight: Math.max(Math.round(height * 0.8), 48)

                    Row {
                        anchors.fill: parent
                        anchors.margins: 8
                        spacing: 8

                        Item {
                            id: detailsSlot
                            width: Math.max(120, parent.width - rightActions.implicitWidth - 8)
                            height: parent.height

                            Column {
                                id: detailsColumn
                                anchors.verticalCenter: parent.verticalCenter
                                width: parent.width
                                spacing: 2

                                Row {
                                    width: parent.width
                                    spacing: 6

                                    Text {
                                        text: Name || URI || "Unknown track"
                                        color: "#f0f5ff"
                                        elide: Text.ElideRight
                                        width: parent.width
                                    }
                                }

                                Text {
                                    text: ArtistText || URI || ""
                                    color: "#8ea4c2"
                                    elide: Text.ElideRight
                                    width: parent.width
                                }
                            }
                        }

                        Row {
                            id: rightActions
                            y: Math.floor((parent.height - height) / 2)
                            height: actionButtonHeight
                            spacing: 8

                            Item {
                                visible: isDownloaded
                                width: visible ? (downloadedText.implicitWidth + 10) : 0
                                height: parent.height

                                Rectangle {
                                    id: downloadedBadge
                                    anchors.verticalCenter: parent.verticalCenter
                                    width: parent.width
                                    height: Math.max(20, Math.round(actionButtonHeight * 0.56))
                                    radius: 4
                                    color: "#1f7a3a"
                                    border.color: "#36a85a"
                                    border.width: 1

                                    Text {
                                        id: downloadedText
                                        anchors.centerIn: parent
                                        text: "Downloaded"
                                        color: "#e9ffe9"
                                        font.pixelSize: 10
                                    }
                                }
                            }

                            Item {
                                id: downloadSlot
                                width: visible ? 36 : 0
                                height: rightActions.height
                                visible: !isDownloaded

                                Button {
                                    id: downloadActionButton
                                    anchors.fill: parent
                                    enabled: !spotifyAuthBridge.isDownloadingTrack
                                    onClicked: spotifyAuthBridge.downloadTrackRequest = (URI || "") + "\n" + (Name || "Track") + "\n" + Date.now()

                                    contentItem: Item {
                                        id: downloadContent
                                        anchors.fill: parent
                                        property color iconColor: downloadActionButton.enabled ? actionButtonTextColor : actionButtonTextColorDisabled

                                        Text {
                                            anchors.centerIn: parent
                                            visible: spotifyAuthBridge.isDownloadingTrack
                                            text: "..."
                                            color: downloadContent.iconColor
                                            font.pixelSize: 14
                                        }

                                        Item {
                                            anchors.centerIn: parent
                                            visible: !spotifyAuthBridge.isDownloadingTrack
                                            width: 18
                                            height: 18

                                            Shape {
                                                anchors.fill: parent
                                                antialiasing: true

                                                ShapePath {
                                                    strokeColor: downloadContent.iconColor
                                                    strokeWidth: 2
                                                    fillColor: "transparent"
                                                    capStyle: ShapePath.RoundCap
                                                    joinStyle: ShapePath.RoundJoin
                                                    startX: 9
                                                    startY: 2
                                                    PathLine { x: 9; y: 12 }
                                                    PathLine { x: 5; y: 8 }
                                                    PathMove { x: 9; y: 12 }
                                                    PathLine { x: 13; y: 8 }
                                                }

                                                ShapePath {
                                                    strokeColor: downloadContent.iconColor
                                                    strokeWidth: 2
                                                    fillColor: "transparent"
                                                    capStyle: ShapePath.RoundCap
                                                    startX: 3
                                                    startY: 16
                                                    PathLine { x: 15; y: 16 }
                                                }
                                            }
                                        }
                                    }
                                }
                            }

                            Button {
                                id: rowPlayButton
                                width: Math.max(84, implicitWidth + 16)
                                height: rightActions.height
                                text: "Play"
                                font.pixelSize: 13
                                enabled: isDownloaded ? true : !spotifyAuthBridge.isStreamingTrack
                                contentItem: Text {
                                    text: rowPlayButton.text
                                    horizontalAlignment: Text.AlignHCenter
                                    verticalAlignment: Text.AlignVCenter
                                    color: rowPlayButton.enabled ? actionButtonTextColor : actionButtonTextColorDisabled
                                    font.pixelSize: rowPlayButton.font.pixelSize
                                    elide: Text.ElideRight
                                }
                                onClicked: playTrackAtIndex(index)
                            }
                        }
                    }
                }
            }

            Rectangle {
                id: nowPlayingPanel
                width: parent.width
                height: 74
                color: "#0d141f"
                border.color: "#22324d"
                border.width: 1

                Row {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 10

                    Rectangle {
                        width: 56
                        height: 56
                        radius: 6
                        color: "#1a2638"
                        border.color: "#2a3c59"
                        border.width: 1
                        clip: true

                        Image {
                            id: nowPlayingArtImage
                            anchors.fill: parent
                            source: currentTrackAlbumArtUrl
                            fillMode: Image.PreserveAspectCrop
                            asynchronous: true
                            cache: true
                            visible: source.toString().length > 0 && status === Image.Ready
                        }

                        Text {
                            anchors.centerIn: parent
                            visible: !nowPlayingArtImage.visible
                            text: "♪"
                            color: "#8ea4c2"
                            font.pixelSize: 20
                        }
                    }

                    Column {
                        width: Math.max(120, parent.width - 56 - 10)
                        anchors.verticalCenter: parent.verticalCenter
                        spacing: 2

                        Text {
                            width: parent.width
                            text: currentTrackTitle.length > 0 ? currentTrackTitle : "Nothing playing"
                            color: "#e8edf5"
                            elide: Text.ElideRight
                            font.pixelSize: 14
                            font.bold: true
                        }

                        Text {
                            width: parent.width
                            text: currentTrackArtist
                            color: "#8ea4c2"
                            elide: Text.ElideRight
                            font.pixelSize: 12
                            visible: currentTrackArtist.length > 0
                        }
                    }
                }
            }

            Rectangle {
                id: playbackPanel
                width: parent.width
                height: 122
                color: "#101724"
                border.color: "#25344f"
                border.width: 1

                Column {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 8

                    Rectangle {
                        id: progressTrack
                        width: parent.width
                        height: 14
                        radius: 7
                        color: "#182232"

                        Rectangle {
                            width: parent.width * bufferedProgress
                            height: parent.height
                            radius: 7
                            color: "#5f7391"
                        }

                        Rectangle {
                            width: parent.width * playbackProgress
                            height: parent.height
                            radius: 7
                            color: "#9ad0ff"
                        }

                        MouseArea {
                            anchors.fill: parent
                            anchors.margins: -10
                            onPressed: function(mouse) {
                                if (!seekEnabledForCurrentSource) {
                                    spotifyAuthBridge.trackListStatus = "Seek is available after stream buffering finishes"
                                    return
                                }
                                scrubbingActive = true
                                resumeAfterScrub = localPlayer.playbackState === MediaPlayer.PlayingState
                                if (resumeAfterScrub) {
                                    localPlayer.pause()
                                }
                                seekToX(mouse.x, progressTrack.width)
                            }
                            onPositionChanged: function(mouse) {
                                if (mouse.buttons & Qt.LeftButton) {
                                    seekToX(mouse.x, progressTrack.width)
                                }
                            }
                            onReleased: {
                                scrubbingActive = false
                                if (resumeAfterScrub) {
                                    localPlayer.play()
                                }
                                resumeAfterScrub = false
                            }
                            onCanceled: {
                                scrubbingActive = false
                                if (resumeAfterScrub) {
                                    localPlayer.play()
                                }
                                resumeAfterScrub = false
                            }
                            cursorShape: Qt.PointingHandCursor
                        }
                    }

                    Row {
                        width: parent.width

                        Text {
                            id: leftTime
                            text: formatMs(localPlayer.position)
                            color: "#8ea4c2"
                            font.pixelSize: 11
                        }

                        Item {
                            width: Math.max(0, parent.width - leftTime.width - rightTime.width)
                            height: 1
                        }

                        Text {
                            id: rightTime
                            text: formatMs(effectiveDurationMs)
                            color: "#8ea4c2"
                            font.pixelSize: 11
                        }
                    }

                    Item {
                        width: parent.width
                        height: 40

                        Row {
                            anchors.centerIn: parent
                            spacing: 10

                            Button {
                                width: 96
                                text: "Previous"
                                enabled: currentTrackIndex > 0
                                onClicked: playAdjacentTrack(-1)
                            }

                            Button {
                                width: 96
                                text: localPlayer.playbackState === MediaPlayer.PlayingState ? "Pause" : "Play"
                                enabled: localPlayer.source && localPlayer.source.toString().length > 0
                                onClicked: togglePlayPause()
                            }

                            Button {
                                width: 96
                                text: "Next"
                                enabled: currentTrackIndex >= 0 && currentTrackIndex < (trackListModel.count - 1)
                                onClicked: playAdjacentTrack(1)
                            }
                        }
                    }
                }
            }
        }
    }

}
