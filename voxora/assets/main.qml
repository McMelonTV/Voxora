import QtQuick
import QtQuick.Controls
import QtMultimedia
import QtQuick.Window
import "components"

Window {
    id: root
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
    property var localPlayer: playerEngine.player
    property int effectiveDurationMs: Math.max(localPlayer.duration, expectedDurationMs)
    property int pendingResumePositionMs: -1
    property int pendingSeekTargetMs: -1
    property real pendingSeekRatio: -1
    property int pendingResumeAttempts: 0
    property int lastHandledActionNonce: 0
    property real userVolume: 0.8
    property bool isDraggingVolumeSlider: false
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property string currentTrackTitle: ""
    property string currentTrackArtist: ""
    property string currentTrackAlbumArtUrl: ""
    property string currentTrackURI: ""
    property string currentTrackDownloadedPath: ""
    property int currentTrackIndex: -1
    property bool autoplayEnabled: true
    property bool dataSavingMode: false
    property int prebufferAheadCount: 3
    property bool streamShouldAutoPlay: true
    property bool holdStoppedTrackState: false
    property string activeContextURI: ""
    property string activeContextName: ""
    property int lastHandledSessionLoadedNonce: 0
    property int bridgeSessionLoadedNonce: Number(spotifyAuthBridge.sessionLoadedNonce) || 0
    property int bridgeActionNonce: 0
    property real uiPendingVolume: 0.8
    property bool uiPendingDataSavingMode: false
    property string uiPendingCollectionURI: ""
    property string uiPendingCollectionName: ""
    property int uiPendingTrackIndex: -1
    property string uiPendingDownloadURI: ""
    property string uiPendingDownloadName: ""
    property real uiPendingSeekRatio: -1
    property string uiPendingStreamTrackURI: ""
    property string uiPendingStreamTrackName: ""
    property bool uiPendingLikelyNaturalEnd: false
    property bool uiPendingHasNewBufferedData: false
    property int sessionPendingPositionMs: 0

    signal uiConnectSpotifyRequested()
    signal uiClearStreamCacheRequested()
    signal uiVolumeChangedRequested()
    signal uiDataSavingToggledRequested()
    signal uiOpenCollectionRequested()
    signal uiNavigateBackRequested()
    signal uiLoadMoreRequested()
    signal uiPlayTrackRequested()
    signal uiDownloadTrackRequested()
    signal uiPreviousRequested()
    signal uiNextRequested()
    signal uiLoadSessionRequested()
    signal uiPersistSessionRequested()
    signal uiSeekRequested()
    signal uiClearStreamRequested()
    signal uiStreamTrackRequested()
    signal uiComputePrebufferRequested()
    signal uiEvaluateRecoveryRequested()
    property real playbackProgress: effectiveDurationMs > 0 ? Math.max(0, Math.min(1, localPlayer.position / effectiveDurationMs)) : 0
    property bool usingStreamSource: {
        if (currentPlayingPath.length === 0) {
            return false
        }
        var playPath = spotifyAuthBridge.streamPlayPath || ""
        var cachePath = spotifyAuthBridge.streamCacheFilePath || spotifyAuthBridge.streamCachePath || ""
        return currentPlayingPath === playPath || (cachePath.length > 0 && currentPlayingPath === cachePath)
    }
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
    property bool streamCacheReady: !!spotifyAuthBridge.streamCacheReady
    property int streamBufferedBytes: Number(spotifyAuthBridge.streamBufferedBytes) || 0
    property int streamBufferedTotal: Number(spotifyAuthBridge.streamBufferedTotal) || 0
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

    Item {
        anchors.fill: parent
        focus: true
        Keys.onReleased: function(event) {
            if ((event.key === Qt.Key_Back || event.key === Qt.Key_Escape) && controller.handleBackNavigation()) {
                event.accepted = true
            }
        }
    }

    onClosing: function(close) {
        if (close.accepted) {
            controller.persistSessionState()
        }
        if (controller.handleBackNavigation()) {
            close.accepted = false
        }
    }

    Shortcut {
        sequence: "Esc"
        context: Qt.ApplicationShortcut
        onActivated: controller.handleBackNavigation()
    }

    Shortcut {
        sequence: "Back"
        context: Qt.ApplicationShortcut
        onActivated: controller.handleBackNavigation()
    }

    onStreamFetchProgressChanged: {
        if (streamFetchProgress >= 0) {
            bufferedProgressLatched = Math.max(bufferedProgressLatched, streamFetchProgress)
        }
    }

    Connections {
        target: spotifyAuthBridge
        function onActionNonceChanged() {
            bridgeActionNonce = Number(spotifyAuthBridge.actionNonce) || 0
        }
        function onSessionUserVolumeChanged() {
            if (root.isDraggingVolumeSlider) {
                return
            }
            var next = Number(spotifyAuthBridge.sessionUserVolume)
            if (!isNaN(next)) {
                userVolume = Math.max(0, Math.min(1, next))
            }
        }
        function onSessionDataSavingModeChanged() {
            dataSavingMode = !!spotifyAuthBridge.sessionDataSavingMode
        }
    }

    onBridgeSessionLoadedNonceChanged: {
        var sessionNonce = bridgeSessionLoadedNonce
        if (sessionNonce === lastHandledSessionLoadedNonce) {
            return
        }
        lastHandledSessionLoadedNonce = sessionNonce
        controller.restoreSessionState()
    }

    onBridgeActionNonceChanged: {
        controller.handleBridgeAction()
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

    // Minimal host contract used by PlayerEngine/MainController.
    function recoverStreamingPlayback() {
        controller.recoverStreamingPlayback()
    }

    function refreshPartStreamSource(targetPosMs) {
        controller.refreshPartStreamSource(targetPosMs)
    }

    function requestNextTrackPrebuffer() {
        controller.requestNextTrackPrebuffer()
    }

    function persistSessionState() {
        controller.persistSessionState()
    }

    function restoreSessionState() {
        controller.restoreSessionState()
    }

    function handleBackNavigation() {
        return controller.handleBackNavigation()
    }

    function applyPendingResume() {
        controller.applyPendingResume()
    }

    MainController {
        id: controller
        host: root
        bridge: spotifyAuthBridge
        player: localPlayer
        engine: playerEngine
    }

    PlayerEngine {
        id: playerEngine
        host: root
        bridge: spotifyAuthBridge
    }

    Component.onCompleted: {
        uiLoadSessionRequested()
    }

    ContentPanels {
        anchors.fill: parent
        bridge: spotifyAuthBridge
        currentTrackIndex: root.currentTrackIndex
        actionButtonTextColor: root.actionButtonTextColor
        actionButtonTextColorDisabled: root.actionButtonTextColorDisabled
        currentTrackAlbumArtUrl: root.currentTrackAlbumArtUrl
        currentTrackTitle: root.currentTrackTitle
        currentTrackArtist: root.currentTrackArtist
        userVolume: root.userVolume
        dataSavingMode: root.dataSavingMode
        localPlayer: playerEngine.player
        bufferedProgress: root.bufferedProgress
        playbackProgress: root.playbackProgress
        effectiveDurationMs: root.effectiveDurationMs
        seekEnabledForCurrentSource: root.seekEnabledForCurrentSource
        formatMsFn: root.formatMs

        onConnectSpotifyRequested: root.uiConnectSpotifyRequested()
        onClearStreamCacheRequested: root.uiClearStreamCacheRequested()
        onUserVolumeChangedByUser: function(value) {
            var nextVolume = Number(value)
            if (isNaN(nextVolume)) {
                nextVolume = 0.8
            }
            root.userVolume = Math.max(0, Math.min(1, nextVolume))
            root.uiPendingVolume = value
            root.uiVolumeChangedRequested()
        }
        onUserVolumeDragStateChanged: function(dragging) {
            root.isDraggingVolumeSlider = !!dragging
        }
        onDataSavingModeToggled: {
            root.dataSavingMode = !root.dataSavingMode
            root.uiPendingDataSavingMode = root.dataSavingMode
            root.uiDataSavingToggledRequested()
        }
        onOpenCollectionRequested: function(uri, name) {
            root.uiPendingCollectionURI = (uri || "").trim()
            root.uiPendingCollectionName = name || "Collection"
            root.uiOpenCollectionRequested()
        }
        onNavigateBackRequested: root.uiNavigateBackRequested()
        onLoadMoreRequested: root.uiLoadMoreRequested()
        onPlayTrackRequested: function(index) {
            root.uiPendingTrackIndex = index
            root.uiPlayTrackRequested()
        }
        onDownloadTrackRequested: function(uri, name) {
            root.uiPendingDownloadURI = uri || ""
            root.uiPendingDownloadName = name || "Track"
            root.uiDownloadTrackRequested()
        }
        onSeekPressed: function(mouseX, width) {
            if (!root.seekEnabledForCurrentSource) {
                spotifyAuthBridge.trackListStatus = "Seek is available after stream buffering finishes"
                return
            }
            root.scrubbingActive = true
            root.resumeAfterScrub = localPlayer.playbackState === MediaPlayer.PlayingState
            if (root.resumeAfterScrub) {
                localPlayer.pause()
            }
            controller.seekToX(mouseX, width)
        }
        onSeekMoved: function(mouseX, width, buttons) {
            if (buttons & Qt.LeftButton) {
                controller.seekToX(mouseX, width)
            }
        }
        onSeekReleased: {
            root.scrubbingActive = false
            if (root.resumeAfterScrub) {
                localPlayer.play()
            }
            root.resumeAfterScrub = false
        }
        onSeekCanceled: {
            root.scrubbingActive = false
            if (root.resumeAfterScrub) {
                localPlayer.play()
            }
            root.resumeAfterScrub = false
        }
        onPreviousRequested: root.uiPreviousRequested()
        onPlayPauseRequested: {
            if (localPlayer.source && localPlayer.source.toString().length > 0) {
                holdStoppedTrackState = false
                if (localPlayer.playbackState === MediaPlayer.PlayingState) {
                    localPlayer.pause()
                } else {
                    localPlayer.play()
                }
            }
        }
        onNextRequested: root.uiNextRequested()
    }

}
