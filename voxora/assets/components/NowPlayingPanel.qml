import QtQuick
import QtQuick.Controls

Pane {
    id: root

    property string currentTrackAlbumArtUrl: ""
    property string currentTrackTitle: ""
    property string currentTrackArtist: ""

    padding: 8

    background: Rectangle {
        color: "#0d141f"
        border.color: "#22324d"
        border.width: 1
    }

    contentItem: Row {
        id: nowPlayingRow
        width: root.availableWidth
        spacing: 10

        Rectangle {
            width: 56
            height: 56
            radius: 6
            color: "#1a2638"
            border.color: "#2a3c59"
            border.width: 1
            clip: true

            Image {
                id: nowPlayingArtImage
                anchors.fill: parent
                source: root.currentTrackAlbumArtUrl
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                visible: source.toString().length > 0 && status === Image.Ready
            }

            Text {
                anchors.centerIn: parent
                visible: !nowPlayingArtImage.visible
                text: "♪"
                color: "#8ea4c2"
                font.pixelSize: 20
            }
        }

        Column {
            width: Math.max(120, root.availableWidth - 56 - 10)
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2

            Text {
                width: parent.width
                text: root.currentTrackTitle.length > 0 ? root.currentTrackTitle : "Nothing playing"
                color: "#e8edf5"
                elide: Text.ElideRight
                font.pixelSize: 14
                font.bold: true
            }

            Text {
                width: parent.width
                text: root.currentTrackArtist
                color: "#8ea4c2"
                elide: Text.ElideRight
                font.pixelSize: 12
                visible: root.currentTrackArtist.length > 0
            }
        }
    }
}
