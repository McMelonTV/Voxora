import QtQuick
import QtQuick.Controls
import QtMultimedia
import QtQuick.Window

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
    property bool partFinalizeRefreshDone: false
    property int expectedDurationMs: 0
    property int effectiveDurationMs: Math.max(localPlayer.duration, expectedDurationMs)
    property int pendingResumePositionMs: -1
    property int pendingSeekTargetMs: -1
    property real pendingSeekRatio: -1
    property int pendingResumeAttempts: 0
    property bool restoreTrackListScrollPending: false
    property real restoreTrackListScrollY: 0
    property int restoreTrackListScrollAttempts: 0
    property real userVolume: 0.8
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
        if (path.startsWith("file://")) {
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
        var cachePath = spotifyAuthBridge.streamCachePath || ""
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
        if (!targetPath.endsWith(".stream.ogg.part")) {
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

        var cachePath = spotifyAuthBridge.streamCachePath || ""
        var isLiveFifo = currentPlayingPath.endsWith(".live.fifo")
        var isPartStream = currentPlayingPath.endsWith(".stream.ogg.part")

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
        if (localPlayer.playbackState === MediaPlayer.PlayingState) {
            localPlayer.pause()
        } else {
            localPlayer.play()
        }
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

    function requestLoadMoreTracksPreserveScroll() {
        if (spotifyAuthBridge.isLoadingTracks || !spotifyAuthBridge.trackHasMore) {
            return
        }
        var lv = activeTrackListView()
        if (lv) {
            restoreTrackListScrollY = lv.contentY
            restoreTrackListScrollPending = true
            restoreTrackListScrollAttempts = 0
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
        }

        pendingResumeAttempts += 1
        if (pendingResumeAttempts > 60) {
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
            if (!streamSourceSwitching && !scrubbingActive && currentPlayingPath.endsWith(".stream.ogg.part")) {
                var total = Number(spotifyAuthBridge.streamBufferedTotal)
                var have = Number(spotifyAuthBridge.streamBufferedBytes)
                if (!!spotifyAuthBridge.isStreamingTrack && !isNaN(total) && !isNaN(have) && total > 0 && effectiveDurationMs > 0) {
                    var bufferedRatio = Math.max(0, Math.min(1, have / total))
                    var bufferedMs = Math.floor(effectiveDurationMs * bufferedRatio)
                    if (bufferedMs > 0 && localPlayer.position >= Math.max(0, bufferedMs - 2500)) {
                        refreshPartStreamSource(localPlayer.position)
                    }
                } else if (!spotifyAuthBridge.isStreamingTrack && !partFinalizeRefreshDone && localPlayer.position > 0) {
                    partFinalizeRefreshDone = true
                    refreshPartStreamSource(localPlayer.position)
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
                streamRecovering = false
                streamRecoverAttempts = 0
                streamRecoverTargetPositionMs = -1
            }

            if (playbackState === MediaPlayer.StoppedState) {
                if (streamSourceSwitching) {
                    return
                }
                var cachePath = spotifyAuthBridge.streamCachePath || ""
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
        id: restoreTrackListScrollTimer
        interval: 16
        repeat: true
        running: false
        onTriggered: {
            var lv = activeTrackListView()
            if (!restoreTrackListScrollPending || !lv) {
                stop()
                return
            }

            var maxY = Math.max(0, lv.contentHeight - lv.height)
            var targetY = Math.max(0, Math.min(restoreTrackListScrollY, maxY))
            lv.contentY = targetY
            restoreTrackListScrollAttempts += 1

            if (Math.abs(lv.contentY - targetY) <= 1 || restoreTrackListScrollAttempts > 12) {
                restoreTrackListScrollPending = false
                stop()
            }
        }
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
                    partFinalizeRefreshDone = false
                    streamOpenBufferedBytes = Number(spotifyAuthBridge.streamBufferedBytes) || 0
                    bufferedProgressLatched = 0
                    localPlayer.source = ""
                    localPlayer.source = toFileUrl(nextPath)
                    localPlayer.play()
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

    property var trackListData: {
        try {
            return JSON.parse(spotifyAuthBridge.trackListJson)
        } catch (e) {
            return []
        }
    }

    onTrackListDataChanged: {
        if (restoreTrackListScrollPending) {
            restoreTrackListScrollTimer.restart()
        }
    }

    Column {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            width: parent.width
            color: "#161616"
            border.color: "#2b2b2b"
            border.width: 1
            implicitHeight: infoColumn.implicitHeight + 20

            Column {
                id: infoColumn
                anchors.fill: parent
                anchors.margins: 10
                spacing: 4

                Text {
                    text: "Platform: " + graphicsPlatform
                    color: "#f3f3f3"
                }

                Text {
                    text: "Graphics API: " + graphicsApi
                    color: "#d7d7d7"
                }

                Text {
                    text: "Renderer: " + graphicsRenderer
                    color: "#d7d7d7"
                    wrapMode: Text.WrapAnywhere
                }

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
                        width: Math.max(120, infoColumn.width - 170)
                    }
                }

                Text {
                    text: spotifyAuthBridge.libraryStatus
                    color: "#9ad0ff"
                    wrapMode: Text.WrapAnywhere
                }

                Text {
                    text: spotifyAuthBridge.likedSongsName + ": " + spotifyAuthBridge.likedSongsCount + " tracks"
                    color: "#cfd8e6"
                    wrapMode: Text.WrapAnywhere
                }

                Button {
                    text: spotifyAuthBridge.isLoadingTracks ? "Opening..." : "Open Liked Songs"
                    enabled: !spotifyAuthBridge.isLoadingTracks
                    onClicked: spotifyAuthBridge.openCollectionRequest = "spotify:collection:tracks\n" + spotifyAuthBridge.likedSongsName + "\n" + Date.now()
                }
            }
        }

        Item {
            width: parent.width
            height: parent.height - infoColumn.parent.height

            Loader {
                id: contentLoader
                anchors.fill: parent
                sourceComponent: spotifyAuthBridge.viewMode === "tracks" ? trackListComponent : libraryListComponent
            }
        }
    }

    Component {
        id: libraryListComponent

        ListView {
            clip: true
            model: playlistsData
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
                    onClicked: spotifyAuthBridge.openCollectionRequest = (modelData.URI || "") + "\n" + (modelData.Name || "Playlist") + "\n" + Date.now()
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
                        text: "Back"
                        onClicked: spotifyAuthBridge.navigateBackNonce = Date.now()
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
                width: parent.width
                text: spotifyAuthBridge.trackListStatus
                color: "#9ad0ff"
                wrapMode: Text.WrapAnywhere
                padding: 8
            }

            Rectangle {
                width: parent.width
                height: 52
                color: "#0f1520"
                border.color: "#223148"
                border.width: 1

                Row {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 10

                    Button {
                        width: 86
                        text: localPlayer.playbackState === MediaPlayer.PlayingState ? "Pause" : "Play"
                        enabled: localPlayer.source && localPlayer.source.toString().length > 0
                        onClicked: togglePlayPause()
                    }

                    Column {
                        width: Math.max(120, parent.width - 96)
                        spacing: 5

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
                    }
                }
            }

            ListView {
                id: trackListView
                width: parent.width
                height: parent.height - 200
                clip: true
                model: trackListData
                property int autoLoadIssuedForCount: -1

                function requestMoreIfNeeded() {
                    var currentCount = trackListData.length
                    if (currentCount <= 0) {
                        autoLoadIssuedForCount = -1
                        return
                    }
                    var prefetchRows = 10
                    var estimatedRowHeight = 52
                    var prefetchDistance = prefetchRows * estimatedRowHeight
                    var nearEnd = (contentY + height) >= (contentHeight - prefetchDistance)
                    if (currentCount !== autoLoadIssuedForCount && !spotifyAuthBridge.isLoadingTracks && spotifyAuthBridge.trackHasMore && nearEnd) {
                        autoLoadIssuedForCount = currentCount
                        requestLoadMoreTracksPreserveScroll()
                    }
                }

                onContentYChanged: requestMoreIfNeeded()

                onMovementEnded: requestMoreIfNeeded()

                delegate: Rectangle {
                    width: ListView.view ? ListView.view.width : parent.width
                    height: 60
                    color: index % 2 === 0 ? "#0f1623" : "#0d131d"

                    Row {
                        anchors.fill: parent
                        anchors.margins: 8
                        spacing: 8

                        Column {
                            width: Math.max(120, parent.width - 260)
                            spacing: 2

                            Text {
                                text: modelData.Name || modelData.URI || "Unknown track"
                                color: "#f0f5ff"
                                elide: Text.ElideRight
                                width: parent.width
                            }

                            Text {
                                text: modelData.ArtistText || modelData.URI || ""
                                color: "#8ea4c2"
                                elide: Text.ElideRight
                                width: parent.width
                            }
                        }

                        Button {
                            text: spotifyAuthBridge.isDownloadingTrack ? "Downloading..." : "Download"
                            enabled: !spotifyAuthBridge.isDownloadingTrack
                            onClicked: spotifyAuthBridge.downloadTrackRequest = (modelData.URI || "") + "\n" + (modelData.Name || "Track") + "\n" + Date.now()
                        }

                        Button {
                            text: "Play"
                            enabled: modelData.DownloadedPath ? true : !spotifyAuthBridge.isStreamingTrack
                            onClicked: {
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
                                if (modelData.DownloadedPath) {
                                    expectedDurationMs = modelData.DurationMs || 0
                                    currentPlayingPath = modelData.DownloadedPath || ""
                                    localPlayer.source = toFileUrl(currentPlayingPath)
                                    localPlayer.play()
                                } else {
                                    expectedDurationMs = modelData.DurationMs || 0
                                    spotifyAuthBridge.streamTrackRequest = (modelData.URI || "") + "\n" + (modelData.Name || "Track") + "\n" + Date.now()
                                }
                            }
                        }

                        Button {
                            text: "Stop"
                            enabled: localPlayer.playbackState !== MediaPlayer.StoppedState
                            onClicked: {
                                manualStopRequested = true
                                localPlayer.stop()
                                currentPlayingPath = ""
                                expectedDurationMs = 0
                                spotifyAuthBridge.clearStreamNonce = Date.now()
                            }
                        }
                    }
                }
            }

            Rectangle {
                width: parent.width
                height: 52
                color: "#101724"
                border.color: "#25344f"
                border.width: 1

                Row {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 10

                    Button {
                        text: spotifyAuthBridge.isLoadingTracks ? "Loading..." : "Load More"
                        enabled: !spotifyAuthBridge.isLoadingTracks && spotifyAuthBridge.trackHasMore
                        visible: spotifyAuthBridge.trackHasMore || spotifyAuthBridge.isLoadingTracks
                        onClicked: requestLoadMoreTracksPreserveScroll()
                    }

                    Text {
                        text: spotifyAuthBridge.trackTotal > 0
                              ? (trackListData.length + " / " + spotifyAuthBridge.trackTotal)
                              : (trackListData.length + " tracks")
                        color: "#8ea4c2"
                        verticalAlignment: Text.AlignVCenter
                    }

                    Text {
                        text: "Volume"
                        color: "#8ea4c2"
                        verticalAlignment: Text.AlignVCenter
                    }

                    Slider {
                        width: 140
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
    }
}
