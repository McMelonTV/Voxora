import QtQuick

ListView {
    id: root

    property var bridge
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property int autoLoadIssuedForCount: -1
    property bool autoLoadAwaitUserScroll: false

    signal loadMoreRequested(bool forceRequest)
    signal playRequested(int index)
    signal downloadRequested(string uri, string name)

    clip: true
    reuseItems: true
    cacheBuffer: 0
    displayMarginBeginning: 0
    displayMarginEnd: 0

    function requestMoreIfNeeded() {
        var currentCount = count
        if (currentCount <= 0) {
            autoLoadIssuedForCount = -1
            autoLoadAwaitUserScroll = false
            return
        }
        var prefetchRows = 10
        var estimatedRowHeight = 52
        var prefetchDistance = prefetchRows * estimatedRowHeight
        var nearEnd = (contentY + height) >= (contentHeight - prefetchDistance)
        if (!autoLoadAwaitUserScroll && currentCount !== autoLoadIssuedForCount && !(root.bridge && root.bridge.isLoadingTracks) && root.bridge && root.bridge.trackHasMore && nearEnd) {
            autoLoadIssuedForCount = currentCount
            autoLoadAwaitUserScroll = true
            loadMoreRequested(false)
        }
    }

    onMovementStarted: {
        autoLoadAwaitUserScroll = false
    }

    onMovementEnded: requestMoreIfNeeded()

    delegate: TrackListDelegate {
        width: ListView.view ? ListView.view.width : root.width
        actionButtonTextColor: root.actionButtonTextColor
        actionButtonTextColorDisabled: root.actionButtonTextColorDisabled
        isDownloadingTrack: root.bridge && root.bridge.isDownloadingTrack
        isStreamingTrack: root.bridge && root.bridge.isStreamingTrack
        nameText: Name || ""
        uriText: URI || ""
        artistText: ArtistText || ""
        downloadedPath: DownloadedPath || ""
        itemIndex: index
        onDownloadRequested: function(uri, name) {
            root.downloadRequested(uri, name)
        }
        onPlayRequested: function(index) {
            root.playRequested(index)
        }
    }
}
