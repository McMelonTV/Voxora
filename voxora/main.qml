import QtQuick
import QtQuick.Controls
import QtQuick.Window

Window {
    width: 576
    height: 1024
    visible: true

    title: "Voxora"
    color: "#0f1115"

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
            }
        }

        ListView {
            width: parent.width
            height: parent.height - infoColumn.parent.height
            clip: true
            model: myModel
            delegate: Row {
                spacing: 10
                Text {
                    text: display
                }
            }
        }
    }
}
