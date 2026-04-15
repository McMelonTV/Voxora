import QtQuick

Column {
    id: root

    property alias trackListViewRef: trackListView
    property var bridge
    property var trackListModel
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property string currentTrackAlbumArtUrl: ""
    property string currentTrackTitle: ""
    property string currentTrackArtist: ""
    property int currentTrackIndex: -1
    property real playbackProgress: 0
    property real bufferedProgress: 0
    property int effectiveDurationMs: 0
    property bool seekEnabledForCurrentSource: true
    property var localPlayer

    signal backRequested()
    signal loadMoreRequested(bool forceRequest)
    signal playRequested(int index)
    signal downloadRequested(string uri, string name)
    signal togglePlayPauseRequested()
    signal playAdjacentRequested(int step)
    signal scrubPressed(real mouseX, real trackWidth)
    signal scrubMoved(real mouseX, real trackWidth)
    signal scrubReleased()
    signal scrubCanceled()
    signal statusMessageRequested(string message)

    spacing: 0

    TrackHeader {
        id: trackHeader
        width: parent.width
        actionButtonTextColor: root.actionButtonTextColor
        titleText: root.bridge ? root.bridge.trackListTitle : ""
        onBackRequested: root.backRequested()
    }

    Text {
        id: trackStatusText
        width: parent.width
        text: root.bridge ? root.bridge.trackListStatus : ""
        color: "#9ad0ff"
        wrapMode: Text.WrapAnywhere
        padding: 8
    }

    TrackListView {
        id: trackListView
        width: parent.width
        height: Math.max(120, parent.height - trackHeader.height - trackStatusText.implicitHeight - nowPlayingPanel.height - playbackPanel.height)
        model: root.trackListModel
        bridge: root.bridge
        actionButtonTextColor: root.actionButtonTextColor
        actionButtonTextColorDisabled: root.actionButtonTextColorDisabled
        onLoadMoreRequested: function(forceRequest) {
            root.loadMoreRequested(forceRequest)
        }
        onPlayRequested: function(index) {
            root.playRequested(index)
        }
        onDownloadRequested: function(uri, name) {
            root.downloadRequested(uri, name)
        }
    }

    NowPlayingPanel {
        id: nowPlayingPanel
        width: parent.width
        albumArtUrl: root.currentTrackAlbumArtUrl
        trackTitle: root.currentTrackTitle
        trackArtist: root.currentTrackArtist
    }

    PlaybackPanel {
        id: playbackPanel
        width: parent.width
        bufferedProgress: root.bufferedProgress
        playbackProgress: root.playbackProgress
        seekEnabledForCurrentSource: root.seekEnabledForCurrentSource
        positionMs: root.localPlayer ? root.localPlayer.position : 0
        effectiveDurationMs: root.effectiveDurationMs
        currentTrackIndex: root.currentTrackIndex
        trackCount: root.trackListModel ? root.trackListModel.count : 0
        player: root.localPlayer
        onPlayPreviousRequested: root.playAdjacentRequested(-1)
        onTogglePlayPauseRequested: root.togglePlayPauseRequested()
        onPlayNextRequested: root.playAdjacentRequested(1)
        onScrubPressed: function(mouseX, trackWidth) {
            root.scrubPressed(mouseX, trackWidth)
        }
        onScrubMoved: function(mouseX, trackWidth) {
            root.scrubMoved(mouseX, trackWidth)
        }
        onScrubReleased: root.scrubReleased()
        onScrubCanceled: root.scrubCanceled()
        onStatusMessageRequested: function(message) {
            root.statusMessageRequested(message)
        }
    }
}
