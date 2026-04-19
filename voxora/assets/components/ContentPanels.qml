import QtQuick
import QtMultimedia

Item {
    id: root

    property var bridge
    property var trackListModel
    property int currentTrackIndex: -1
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property string currentTrackAlbumArtUrl: ""
    property string currentTrackTitle: ""
    property string currentTrackArtist: ""
    property real userVolume: 0.8
    property bool dataSavingMode: false
    property var localPlayer
    property real bufferedProgress: 0
    property real playbackProgress: 0
    property int effectiveDurationMs: 0
    property bool seekEnabledForCurrentSource: true
    property var formatMsFn

    signal connectSpotifyRequested()
    signal clearStreamCacheRequested()
    signal userVolumeChangedByUser(real value)
    signal dataSavingModeToggled()
    signal openCollectionRequested(string uri, string name)

    signal navigateBackRequested()
    signal loadMoreRequested()
    signal playTrackRequested(int index)
    signal downloadTrackRequested(string uri, string name)

    signal seekPressed(real mouseX, real width)
    signal seekMoved(real mouseX, real width, int buttons)
    signal seekReleased()
    signal seekCanceled()
    signal previousRequested()
    signal playPauseRequested()
    signal nextRequested()

    Loader {
        id: contentLoader
        anchors.fill: parent
        sourceComponent: bridge && bridge.viewMode === "tracks" ? trackListComponent : libraryListComponent
    }

    Component {
        id: libraryListComponent

        LibraryListPanel {
            anchors.fill: parent
            bridge: root.bridge
            libraryListEntries: {
                var playlists = []
                try {
                    playlists = JSON.parse(root.bridge.playlistsJson)
                } catch (e) {
                    playlists = []
                }
                var base = []
                base.push({
                    Name: root.bridge.likedSongsName || "Liked Songs",
                    URI: "spotify:collection:tracks",
                    TrackCount: Number(root.bridge.likedSongsCount) || 0,
                    OwnerName: "Your Library"
                })
                for (var i = 0; i < playlists.length; i += 1) {
                    base.push(playlists[i])
                }
                return base
            }
            userVolume: root.userVolume
            dataSavingMode: root.dataSavingMode

            onConnectSpotifyRequested: root.connectSpotifyRequested()
            onClearStreamCacheRequested: root.clearStreamCacheRequested()
            onUserVolumeChangedByUser: function(value) {
                root.userVolumeChangedByUser(value)
            }
            onDataSavingModeToggled: root.dataSavingModeToggled()
            onOpenCollectionRequested: function(uri, name) {
                root.openCollectionRequested(uri, name)
            }
        }
    }

    Component {
        id: trackListComponent

        Column {
            id: trackListRoot
            anchors.fill: parent
            spacing: 0

            ListModel {
                id: internalTrackModel
            }

            property var trackListData: {
                try {
                    return JSON.parse(root.bridge.trackListJson)
                } catch (e) {
                    return []
                }
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
                if (incoming.length === 0) {
                    if (internalTrackModel.count > 0) {
                        internalTrackModel.clear()
                    }
                    return
                }
                internalTrackModel.clear()
                for (var i = 0; i < incoming.length; i += 1) {
                    internalTrackModel.append(normalizeTrackItem(incoming[i]))
                }
            }

            onTrackListDataChanged: {
                syncTrackListModel(trackListData)
            }

            TrackListPanel {
                id: trackListPanel
                width: parent.width
                height: Math.max(120, parent.height - nowPlayingPanel.height - playbackPanel.height)
                bridge: root.bridge
                tracksModel: internalTrackModel
                currentTrackIndex: root.currentTrackIndex
                actionButtonTextColor: root.actionButtonTextColor
                actionButtonTextColorDisabled: root.actionButtonTextColorDisabled
                nowPlayingPanelHeight: nowPlayingPanel.height
                playbackPanelHeight: playbackPanel.height

                onBackRequested: root.navigateBackRequested()
                onLoadMoreRequested: root.loadMoreRequested()
                onPlayTrackRequested: function(index) {
                    root.playTrackRequested(index)
                }
                onDownloadTrackRequested: function(uri, name) {
                    root.downloadTrackRequested(uri, name)
                }
            }

            NowPlayingPanel {
                id: nowPlayingPanel
                width: parent.width
                currentTrackAlbumArtUrl: root.currentTrackAlbumArtUrl
                currentTrackTitle: root.currentTrackTitle
                currentTrackArtist: root.currentTrackArtist
            }

            PlaybackPanel {
                id: playbackPanel
                width: parent.width
                currentTrackIndex: root.currentTrackIndex
                trackCount: internalTrackModel.count
                isPlaying: !!root.localPlayer && root.localPlayer.playbackState === MediaPlayer.PlayingState
                hasSource: !!root.localPlayer && !!(root.localPlayer.source && root.localPlayer.source.toString().length > 0)
                bufferedProgress: root.bufferedProgress
                playbackProgress: root.playbackProgress
                leftTimeText: root.formatMsFn ? root.formatMsFn(root.localPlayer ? root.localPlayer.position : 0) : "0:00"
                rightTimeText: root.formatMsFn ? root.formatMsFn(root.effectiveDurationMs) : "0:00"
                seekEnabledForCurrentSource: root.seekEnabledForCurrentSource

                onSeekPressed: function(mouseX, width) {
                    root.seekPressed(mouseX, width)
                }
                onSeekMoved: function(mouseX, width, buttons) {
                    root.seekMoved(mouseX, width, buttons)
                }
                onSeekReleased: root.seekReleased()
                onSeekCanceled: root.seekCanceled()
                onPreviousRequested: root.previousRequested()
                onPlayPauseRequested: root.playPauseRequested()
                onNextRequested: root.nextRequested()
            }
        }
    }
}
