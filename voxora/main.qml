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

    function toFileUrl(path) {
        if (!path) {
            return ""
        }
        if (path.startsWith("file://")) {
            return path
        }
        return "file://" + path
    }

    AudioOutput {
        id: localAudioOutput
        volume: 0.8
    }

    MediaPlayer {
        id: localPlayer
        audioOutput: localAudioOutput
        onPlaybackStateChanged: {
            if (playbackState === MediaPlayer.StoppedState) {
                currentPlayingPath = ""
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

            ListView {
                id: trackListView
                width: parent.width
                height: parent.height - 148
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
                        spotifyAuthBridge.loadMoreTracksNonce = Date.now()
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
                            visible: !!modelData.DownloadedPath
                            enabled: !!modelData.DownloadedPath
                            onClicked: {
                                currentPlayingPath = modelData.DownloadedPath || ""
                                localPlayer.source = toFileUrl(currentPlayingPath)
                                localPlayer.play()
                            }
                        }

                        Button {
                            text: "Stop"
                            visible: !!modelData.DownloadedPath
                            enabled: (currentPlayingPath === (modelData.DownloadedPath || "")) && localPlayer.playbackState !== MediaPlayer.StoppedState
                            onClicked: {
                                localPlayer.stop()
                                currentPlayingPath = ""
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
                        onClicked: spotifyAuthBridge.loadMoreTracksNonce = Date.now()
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
                        value: localAudioOutput.volume
                        onValueChanged: localAudioOutput.volume = value
                    }
                }
            }
        }
    }
}
