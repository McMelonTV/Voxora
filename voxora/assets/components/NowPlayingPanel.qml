import QtQuick

Rectangle {
    id: root

    property string albumArtUrl: ""
    property string trackTitle: ""
    property string trackArtist: ""

    height: 74
    color: "#0d141f"
    border.color: "#22324d"
    border.width: 1

    Row {
        anchors.fill: parent
        anchors.margins: 8
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
                source: root.albumArtUrl
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
            width: Math.max(120, parent.width - 56 - 10)
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2

            Text {
                width: parent.width
                text: root.trackTitle.length > 0 ? root.trackTitle : "Nothing playing"
                color: "#e8edf5"
                elide: Text.ElideRight
                font.pixelSize: 14
                font.bold: true
            }

            Text {
                width: parent.width
                text: root.trackArtist
                color: "#8ea4c2"
                elide: Text.ElideRight
                font.pixelSize: 12
                visible: root.trackArtist.length > 0
            }
        }
    }
}
