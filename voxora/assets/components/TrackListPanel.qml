import QtQuick
import QtQuick.Controls
import QtQuick.Shapes

Column {
    id: root
    objectName: "trackListPanelRoot"
    property var bridge
    property var tracksModel
    property int currentTrackIndex: -1
    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property int nowPlayingPanelHeight: 74
    property int playbackPanelHeight: 122

    property alias trackListViewRef: trackListView

    signal backRequested()
    signal loadMoreRequested()
    signal playTrackRequested(int index)
    signal downloadTrackRequested(string uri, string name)

    width: parent ? parent.width : 0
    height: parent ? Math.max(120, parent.height - nowPlayingPanelHeight - playbackPanelHeight) : 120
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
                objectName: "trackListBackButton"
                width: 44
                height: 36
                onClicked: backRequested()

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
                text: bridge ? bridge.trackListTitle : ""
                color: "#e8edf5"
                width: Math.max(120, parent.width - 120)
                elide: Text.ElideRight
            }
        }
    }

    Text {
        id: trackStatusText
        width: parent.width
        text: bridge ? bridge.trackListStatus : ""
        color: "#9ad0ff"
        wrapMode: Text.WrapAnywhere
        padding: 8
    }

    ListView {
        id: trackListView
        objectName: "trackListView"
        width: parent.width
        height: Math.max(120, parent.height - trackHeader.height - trackStatusText.implicitHeight)
        clip: true
        reuseItems: true
        cacheBuffer: 0
        displayMarginBeginning: 0
        displayMarginEnd: 0
        model: tracksModel
        property int autoLoadIssuedForCount: -1
        property bool autoLoadAwaitUserScroll: false

        function requestMoreIfNeeded() {
            var currentCount = tracksModel ? tracksModel.count : 0
            if (currentCount <= 0) {
                autoLoadIssuedForCount = -1
                autoLoadAwaitUserScroll = false
                return
            }
            var prefetchRows = 10
            var estimatedRowHeight = 52
            var prefetchDistance = prefetchRows * estimatedRowHeight
            var nearEnd = (contentY + height) >= (contentHeight - prefetchDistance)
            if (!autoLoadAwaitUserScroll && currentCount !== autoLoadIssuedForCount && bridge && !bridge.isLoadingTracks && bridge.trackHasMore && nearEnd) {
                autoLoadIssuedForCount = currentCount
                autoLoadAwaitUserScroll = true
                loadMoreRequested()
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
                            objectName: "trackRowDownloadButton"
                            anchors.fill: parent
                            enabled: bridge ? !bridge.isDownloadingTrack : true
                            onClicked: downloadTrackRequested(URI || "", Name || "Track")

                            contentItem: Item {
                                id: downloadContent
                                anchors.fill: parent
                                property color iconColor: downloadActionButton.enabled ? actionButtonTextColor : actionButtonTextColorDisabled

                                Text {
                                    anchors.centerIn: parent
                                    visible: bridge ? bridge.isDownloadingTrack : false
                                    text: "..."
                                    color: downloadContent.iconColor
                                    font.pixelSize: 14
                                }

                                Item {
                                    anchors.centerIn: parent
                                    visible: bridge ? !bridge.isDownloadingTrack : true
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
                        objectName: "trackRowPlayButton"
                        width: Math.max(84, implicitWidth + 16)
                        height: rightActions.height
                        text: "Play"
                        font.pixelSize: 13
                        enabled: isDownloaded ? true : (bridge ? !bridge.isStreamingTrack : true)
                        contentItem: Text {
                            text: rowPlayButton.text
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                            color: rowPlayButton.enabled ? actionButtonTextColor : actionButtonTextColorDisabled
                            font.pixelSize: rowPlayButton.font.pixelSize
                            elide: Text.ElideRight
                        }
                        onClicked: playTrackRequested(index)
                    }
                }
            }
        }
    }
}
