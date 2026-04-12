import QtQuick
import QtQuick.Controls
import QtQuick.Window

Window {
    width: 576
    height: 1024
    visible: true

    title: "Voxora"

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
