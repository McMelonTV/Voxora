import QtQuick
import QtMultimedia

QtObject {
    id: root

    property var host
    property var bridge
    property var player
    property var engine

    function recoverStreamingPlayback() {
        if (!host || !bridge || !player || !engine) {
            return
        }
        if (host.manualStopRequested || host.streamRecovering) {
            return
        }

        var targetPath = host.currentPlayingPath || ""
        if (targetPath.length === 0) {
            return
        }

        host.streamRecovering = true
        host.streamRecoverAttempts += 1
        host.streamRecoverTargetPositionMs = Math.max(0, player.position)

        var currentSource = player.source ? player.source.toString() : ""
        var targetSource = host.toFileUrl(targetPath)
        var isLiveFifo = targetPath.endsWith(".live.fifo")
        var cachePath = bridge.streamCacheFilePath || bridge.streamCachePath || ""
        var usingCachePath = cachePath.length > 0 && targetPath === cachePath
        if (currentSource.length === 0 && targetSource.length > 0) {
            player.source = targetSource
        }

        if (!isLiveFifo && host.streamRecoverTargetPositionMs >= 0) {
            host.pendingResumePositionMs = host.streamRecoverTargetPositionMs
            host.pendingSeekRatio = -1
            host.pendingSeekTargetMs = -1
            host.pendingResumeAttempts = 0

            if (usingCachePath && targetSource.length > 0) {
                host.streamSourceSwitching = true
                host.streamSourceSwitchShouldPlay = true
                player.source = ""
                player.source = targetSource
            }
        }
        engine.restartRecoverTimer()
    }

    function refreshPartStreamSource(targetPosMs) {
        if (!host || !player) {
            return
        }
        if (host.streamSourceSwitching) {
            return
        }
        var targetPath = host.currentPlayingPath || ""
        if (!host.isPartStreamPath(targetPath)) {
            return
        }
        var nowMs = Date.now()
        if ((nowMs - host.lastPartRefreshAtMs) < 1200) {
            return
        }
        host.lastPartRefreshAtMs = nowMs

        var wasPlaying = player.playbackState === MediaPlayer.PlayingState
        host.streamSourceSwitching = true
        host.streamSourceSwitchShouldPlay = wasPlaying
        host.pendingResumePositionMs = Math.max(0, targetPosMs)
        host.pendingSeekRatio = -1
        host.pendingSeekTargetMs = -1
        host.pendingResumeAttempts = 0
        player.source = ""
        player.source = host.toFileUrl(targetPath)
    }

    function seekToX(mouseX, trackWidth) {
        if (!host) {
            return
        }
        if (trackWidth <= 0) {
            return
        }
        var ratio = mouseX / trackWidth
        ratio = Math.max(0, Math.min(1, ratio))
        seekToRatio(ratio)
    }

    function seekToRatio(ratio) {
        if (!host) {
            return
        }
        if (host.effectiveDurationMs <= 0) {
            return
        }
        host.uiPendingSeekRatio = ratio
        host.uiSeekRequested()
    }

    function handleBridgeAction() {
        if (!host || !bridge || !player || !engine) {
            return
        }
        var actionNonce = host.bridgeActionNonce
        if (actionNonce <= 0 || actionNonce === host.lastHandledActionNonce) {
            return
        }
        host.lastHandledActionNonce = actionNonce

        var kind = bridge.actionKind || ""
        var reason = bridge.actionReason || ""
        var targetPath = bridge.actionTargetPath || ""
        var targetPos = Number(bridge.actionTargetPosMs)
        if (isNaN(targetPos)) {
            targetPos = 0
        }
        var targetRatio = Number(bridge.actionTargetRatio)
        if (isNaN(targetRatio)) {
            targetRatio = -1
        }

        if (kind === "seek_denied") {
            return
        }

        if (kind === "seek_switch_cache") {
            if (targetPath.length === 0) {
                return
            }
            host.streamSourceSwitching = true
            host.streamSourceSwitchShouldPlay = player.playbackState === MediaPlayer.PlayingState
            host.pendingSeekRatio = targetRatio
            host.pendingResumePositionMs = targetPos
            host.pendingResumeAttempts = 0
            host.currentPlayingPath = targetPath
            player.source = ""
            player.source = host.toFileUrl(targetPath)
            return
        }

        if (kind === "seek_apply") {
            player.position = Math.max(0, targetPos)
            host.pendingResumePositionMs = -1
            host.pendingSeekTargetMs = -1
            host.pendingSeekRatio = -1
            host.pendingResumeAttempts = 0
            engine.stopResumeTimer()
            return
        }

        if (kind === "recover_stream") {
            recoverStreamingPlayback()
            return
        }

        if (kind === "playback_select") {
            var payload = {
                Index: Number(bridge.actionTrackIndex),
                ShouldPlay: !!bridge.actionShouldPlay,
                Name: bridge.actionTrackName || "",
                URI: bridge.actionTrackURI || "",
                ArtistText: bridge.actionTrackArtistText || "",
                AlbumArtURL: bridge.actionTrackAlbumArtURL || "",
                DownloadedPath: bridge.actionTrackDownloadedPath || "",
                DurationMs: Number(bridge.actionTrackDurationMs) || 0
            }
            applyTrackSelection(payload, payload.ShouldPlay !== false)
            if (reason === "session_resume") {
                var resumePos = Math.max(0, targetPos)
                host.pendingResumePositionMs = resumePos
                host.pendingSeekRatio = -1
                host.pendingSeekTargetMs = -1
                host.pendingResumeAttempts = 0
                engine.restartResumeTimer()
            }
            return
        }
    }

    function applyTrackSelection(trackPayload, shouldPlay) {
        if (!host || !player || !engine) {
            return false
        }
        if (!trackPayload) {
            return false
        }

        var trackUri = trackPayload.URI || ""
        if (trackUri.length === 0) {
            return false
        }
        var trackName = trackPayload.Name || "Track"
        var artistText = trackPayload.ArtistText || ""
        var downloadedPath = trackPayload.DownloadedPath || ""
        var isDownloaded = downloadedPath.length > 0
        var durationMs = trackPayload.DurationMs || 0
        var resolvedIndex = Number(trackPayload.Index)
        if (isNaN(resolvedIndex)) {
            resolvedIndex = host.currentTrackIndex
        }

        if (resolvedIndex < 0) {
            resolvedIndex = host.currentTrackIndex
        }

        host.currentTrackIndex = resolvedIndex
        host.currentTrackURI = trackUri
        host.currentTrackTitle = trackName || trackUri || "Unknown track"
        host.currentTrackArtist = artistText
        host.currentTrackAlbumArtUrl = trackPayload.AlbumArtURL || ""
        host.currentTrackDownloadedPath = downloadedPath
        persistSessionState()

        host.holdStoppedTrackState = !shouldPlay
        host.streamShouldAutoPlay = shouldPlay
        host.manualStopRequested = true
        host.streamRecovering = false
        host.streamRecoverAttempts = 0
        host.streamRecoverTargetPositionMs = -1
        host.pendingResumePositionMs = -1
        host.pendingSeekTargetMs = -1
        host.pendingSeekRatio = -1
        host.pendingResumeAttempts = 0
        engine.stopResumeTimer()
        player.stop()
        host.uiClearStreamRequested()
        host.manualStopRequested = false

        host.expectedDurationMs = durationMs
        if (isDownloaded) {
            host.currentPlayingPath = downloadedPath
            player.source = host.toFileUrl(host.currentPlayingPath)
            if (shouldPlay) {
                player.play()
            }
            requestNextTrackPrebuffer()
            return true
        }

        host.uiPendingStreamTrackURI = trackUri
        host.uiPendingStreamTrackName = trackName
        host.uiStreamTrackRequested()
        requestNextTrackPrebuffer()
        return true
    }

    function requestNextTrackPrebuffer() {
        if (!host) {
            return
        }
        if (host.dataSavingMode || !host.autoplayEnabled || host.currentTrackIndex < 0) {
            return
        }
        host.uiComputePrebufferRequested()
    }

    function persistSessionState() {
        if (!host || !player) {
            return
        }
        if (host.activeContextURI.length === 0 || host.currentTrackURI.length === 0) {
            return
        }
        var persistedPositionMs = Math.max(0, Number(player.position) || 0)
        var persistedUserVolume = Math.max(0, Math.min(1, Number(host.userVolume) || 0.8))

        host.sessionPendingPositionMs = persistedPositionMs
        host.uiPendingVolume = persistedUserVolume
        host.uiPendingDataSavingMode = host.dataSavingMode
        host.uiPersistSessionRequested()
    }

    function restoreSessionState() {
        if (!host || !bridge) {
            return
        }
        var loadedNonce = Number(bridge.sessionLoadedNonce) || 0
        if (loadedNonce <= 0) {
            return
        }

        host.dataSavingMode = !!bridge.sessionDataSavingMode
        var contextURI = (bridge.sessionContextURI || "").trim()
        var trackURI = (bridge.sessionTrackURI || "").trim()

        if (contextURI.length === 0 || trackURI.length === 0) {
            return
        }

        host.activeContextURI = contextURI
        host.activeContextName = (bridge.sessionContextName || "Collection").trim()
        host.userVolume = Math.max(0, Math.min(1, Number(bridge.sessionUserVolume) || host.userVolume))
        host.currentTrackTitle = bridge.sessionTrackName || ""
        host.currentTrackArtist = bridge.sessionTrackArtist || ""
        host.currentTrackAlbumArtUrl = bridge.sessionAlbumArtURL || ""

        host.currentTrackURI = trackURI
    }

    function handleBackNavigation() {
        if (!host || !bridge) {
            return false
        }
        if (bridge.viewMode === "tracks") {
            if (!bridge.navCanGoBack) {
                return false
            }
            host.uiNavigateBackRequested()
            return true
        }
        return false
    }

    function applyPendingResume() {
        if (!host || !player || !engine) {
            return
        }
        if (host.pendingResumePositionMs < 0 && host.pendingSeekRatio < 0) {
            return
        }
        var baseDuration = host.effectiveDurationMs
        var target = host.pendingResumePositionMs
        if (host.pendingSeekRatio >= 0 && baseDuration > 0) {
            target = Math.floor(baseDuration * host.pendingSeekRatio)
        }
        var maxPos = baseDuration > 1000 ? (baseDuration - 250) : target
        target = Math.max(0, Math.min(target, maxPos))

        var readyForSeek = host.seekEnabledForCurrentSource && (player.seekable || (player.duration > 0 && !host.currentPlayingPath.endsWith(".live.fifo")))
        if (readyForSeek) {
            player.position = target
            host.pendingSeekTargetMs = target
        } else if (host.holdStoppedTrackState) {
            return
        }

        host.pendingResumeAttempts += 1
        var maxAttempts = host.holdStoppedTrackState ? 600 : 60
        if (host.pendingResumeAttempts > maxAttempts) {
            host.pendingResumePositionMs = -1
            host.pendingSeekTargetMs = -1
            host.pendingSeekRatio = -1
            host.pendingResumeAttempts = 0
            engine.stopResumeTimer()
        }
    }
}
