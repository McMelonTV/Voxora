import QtQuick
import QtMultimedia

Item {
    id: root

    property var host
    property var bridge
    property alias player: localPlayer

    AudioOutput {
        id: localAudioOutput
        volume: host ? host.userVolume : 0.8

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
            if (!host || !bridge) {
                return
            }
            if (mediaStatus === MediaPlayer.EndOfMedia && host.autoplayEnabled && !host.manualStopRequested && !host.streamSourceSwitching) {
                host.uiNextRequested()
                return
            }
            if (host.holdStoppedTrackState && (mediaStatus === MediaPlayer.LoadedMedia || mediaStatus === MediaPlayer.BufferedMedia || mediaStatus === MediaPlayer.BufferingMedia) && host.pendingResumePositionMs >= 0) {
                localPlayer.position = Math.max(0, host.pendingResumePositionMs)
                host.pendingSeekTargetMs = Math.max(0, host.pendingResumePositionMs)
                if (localPlayer.playbackState === MediaPlayer.StoppedState) {
                    localPlayer.pause()
                }
            }
            if (host.streamSourceSwitching && (mediaStatus === MediaPlayer.LoadedMedia || mediaStatus === MediaPlayer.BufferedMedia || mediaStatus === MediaPlayer.BufferingMedia)) {
                var target = Math.max(0, host.pendingResumePositionMs)
                localPlayer.position = target
                host.pendingSeekTargetMs = target
                host.pendingResumeAttempts = 0
                if (host.streamSourceSwitchShouldPlay) {
                    localPlayer.play()
                }
                host.streamSourceSwitching = false
                host.streamSourceSwitchShouldPlay = false
                resumeSeekTimer.restart()
            }
            host.applyPendingResume()
        }

        onPositionChanged: {
            if (!host || !bridge) {
                return
            }
            if (!host.streamSourceSwitching && !host.scrubbingActive && host.isPartStreamPath(host.currentPlayingPath)) {
                var total = Number(bridge.streamBufferedTotal)
                var have = Number(bridge.streamBufferedBytes)
                if (!!bridge.isStreamingTrack && !isNaN(total) && !isNaN(have) && total > 0 && host.effectiveDurationMs > 0) {
                    var bufferedRatio = Math.max(0, Math.min(1, have / total))
                    var bufferedMs = Math.floor(host.effectiveDurationMs * bufferedRatio)
                    if (bufferedMs > 0 && localPlayer.position >= Math.max(0, bufferedMs - 2500)) {
                        host.refreshPartStreamSource(localPlayer.position)
                    }
                }
            }

            if (host.pendingSeekTargetMs >= 0) {
                if (Math.abs(localPlayer.position - host.pendingSeekTargetMs) <= 1500) {
                    host.pendingResumePositionMs = -1
                    host.pendingSeekTargetMs = -1
                    host.pendingSeekRatio = -1
                    host.pendingResumeAttempts = 0
                    resumeSeekTimer.stop()
                }
            }
        }

        onPlaybackStateChanged: {
            if (!host || !bridge) {
                return
            }
            if (playbackState === MediaPlayer.PlayingState) {
                host.holdStoppedTrackState = false
                host.streamRecovering = false
                host.streamRecoverAttempts = 0
                host.streamRecoverTargetPositionMs = -1
                host.requestNextTrackPrebuffer()
            }

            if (playbackState === MediaPlayer.StoppedState) {
                if (host.holdStoppedTrackState) {
                    return
                }
                if (host.streamSourceSwitching) {
                    return
                }
                var bufferedNow = Number(bridge.streamBufferedBytes) || 0
                var likelyNaturalEnd = host.effectiveDurationMs > 0 && localPlayer.position >= (host.effectiveDurationMs - 1500)
                var hasNewBufferedData = bufferedNow > (host.streamOpenBufferedBytes + 8192)

                host.uiPendingLikelyNaturalEnd = likelyNaturalEnd
                host.uiPendingHasNewBufferedData = hasNewBufferedData
                host.uiEvaluateRecoveryRequested()

                host.currentPlayingPath = ""
                host.manualStopRequested = false
                host.streamRecovering = false
                host.streamRecoverAttempts = 0
                host.streamRecoverTargetPositionMs = -1
                host.streamSourceSwitching = false
                host.streamSourceSwitchShouldPlay = false
            }
        }
    }

    Connections {
        target: localPlayer
        function onBufferProgressChanged() {
            if (!host) {
                return
            }
            var raw = Number(localPlayer.bufferProgress)
            if (!isNaN(raw)) {
                raw = Math.max(0, Math.min(1, raw))
                host.bufferedProgressLatched = Math.max(host.bufferedProgressLatched, raw)
            }
        }
    }

    Timer {
        id: resumeSeekTimer
        interval: 120
        repeat: true
        running: false
        onTriggered: {
            if (host) {
                host.applyPendingResume()
            }
        }
    }

    Timer {
        id: recoverStreamingTimer
        interval: 180
        repeat: false
        onTriggered: {
            if (!host) {
                return
            }
            if (!host.streamRecovering || host.manualStopRequested) {
                host.streamRecovering = false
                return
            }
            localPlayer.play()
            if (!host.currentPlayingPath.endsWith(".live.fifo") && host.streamRecoverTargetPositionMs >= 0) {
                host.pendingResumePositionMs = host.streamRecoverTargetPositionMs
                host.pendingSeekRatio = -1
                host.pendingSeekTargetMs = -1
                host.pendingResumeAttempts = 0
                resumeSeekTimer.restart()
            }
        }
    }

    Timer {
        interval: 200
        running: true
        repeat: true
        onTriggered: {
            if (!host || !bridge) {
                return
            }
            if (bridge.streamPlayReadyNonce > host.lastStreamReadyNonce) {
                host.lastStreamReadyNonce = bridge.streamPlayReadyNonce
                var nextPath = bridge.streamPlayPath || ""
                host.currentPlayingPath = nextPath
                if (nextPath.length > 0) {
                    host.manualStopRequested = false
                    host.streamRecovering = false
                    host.streamRecoverAttempts = 0
                    host.streamRecoverTargetPositionMs = -1
                    host.pendingResumePositionMs = -1
                    host.pendingSeekTargetMs = -1
                    host.pendingSeekRatio = -1
                    host.pendingResumeAttempts = 0
                    resumeSeekTimer.stop()
                    host.lastPartRefreshAtMs = -1000000
                    host.streamOpenBufferedBytes = Number(bridge.streamBufferedBytes) || 0
                    host.bufferedProgressLatched = 0
                    localPlayer.source = ""
                    localPlayer.source = host.toFileUrl(nextPath)
                    if (host.streamShouldAutoPlay) {
                        localPlayer.play()
                    }
                }
            }
        }
    }

    Timer {
        id: sessionPersistTimer
        interval: 1200
        repeat: true
        running: true
        onTriggered: {
            if (!host) {
                return
            }
            if (localPlayer.playbackState === MediaPlayer.PlayingState) {
                host.persistSessionState()
            }
        }
    }

    function restartRecoverTimer() {
        recoverStreamingTimer.restart()
    }

    function stopResumeTimer() {
        resumeSeekTimer.stop()
    }

    function restartResumeTimer() {
        resumeSeekTimer.restart()
    }
}
