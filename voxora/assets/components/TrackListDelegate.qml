import QtQuick
import QtQuick.Controls
import QtQuick.Shapes

Rectangle {
    id: root

    property color actionButtonTextColor: "#1f2a38"
    property color actionButtonTextColorDisabled: "#7d8796"
    property bool isDownloadingTrack: false
    property bool isStreamingTrack: false
    property string nameText: ""
    property string uriText: ""
    property string artistText: ""
    property string downloadedPath: ""
    property int itemIndex: -1
    property bool isDownloaded: !!(downloadedPath && String(downloadedPath).trim().length > 0)
    property int actionButtonHeight: Math.max(Math.round(height * 0.8), 48)

    signal downloadRequested(string uri, string name)
    signal playRequested(int index)

    height: 60
    color: index % 2 === 0 ? "#0f1623" : "#0d131d"

    Row {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 8

        Item {
            width: Math.max(120, parent.width - rightActions.implicitWidth - 8)
            height: parent.height

            Column {
                anchors.verticalCenter: parent.verticalCenter
                width: parent.width
                spacing: 2

                Row {
                    width: parent.width
                    spacing: 6

                    Text {
                        text: root.nameText || root.uriText || "Unknown track"
                        color: "#f0f5ff"
                        elide: Text.ElideRight
                        width: parent.width
                    }
                }

                Text {
                    text: root.artistText || root.uriText || ""
                    color: "#8ea4c2"
                    elide: Text.ElideRight
                    width: parent.width
                }
            }
        }

        Row {
            id: rightActions
            y: Math.floor((parent.height - height) / 2)
            height: root.actionButtonHeight
            spacing: 8

            Item {
                visible: root.isDownloaded
                width: visible ? (downloadedText.implicitWidth + 10) : 0
                height: parent.height

                Rectangle {
                    anchors.verticalCenter: parent.verticalCenter
                    width: parent.width
                    height: Math.max(20, Math.round(root.actionButtonHeight * 0.56))
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
                width: visible ? 36 : 0
                height: rightActions.height
                visible: !root.isDownloaded

                Button {
                    id: downloadActionButton
                    anchors.fill: parent
                    enabled: !root.isDownloadingTrack
                    onClicked: root.downloadRequested(root.uriText, root.nameText || "Track")

                    contentItem: Item {
                        id: downloadContent
                        anchors.fill: parent
                        property color iconColor: downloadActionButton.enabled ? root.actionButtonTextColor : root.actionButtonTextColorDisabled

                        Text {
                            anchors.centerIn: parent
                            visible: root.isDownloadingTrack
                            text: "..."
                            color: downloadContent.iconColor
                            font.pixelSize: 14
                        }

                        Item {
                            anchors.centerIn: parent
                            visible: !root.isDownloadingTrack
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
                enabled: root.isDownloaded ? true : !root.isStreamingTrack
                contentItem: Text {
                    text: rowPlayButton.text
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    color: rowPlayButton.enabled ? root.actionButtonTextColor : root.actionButtonTextColorDisabled
                    font.pixelSize: rowPlayButton.font.pixelSize
                    elide: Text.ElideRight
                }
                onClicked: root.playRequested(root.itemIndex)
            }
        }
    }
}
