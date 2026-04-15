import QtQuick
import QtQuick.Controls
import QtQuick.Shapes

Rectangle {
    id: root

    property color actionButtonTextColor: "#1f2a38"
    property string titleText: ""

    signal backRequested()

    height: 52
    color: "#121c2d"
    border.color: "#2d3f5f"
    border.width: 1

    Row {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 10

        Button {
            width: 44
            height: 36
            onClicked: root.backRequested()

            contentItem: Item {
                anchors.fill: parent

                Shape {
                    anchors.centerIn: parent
                    width: 14
                    height: 14
                    antialiasing: true

                    ShapePath {
                        strokeColor: root.actionButtonTextColor
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
            text: root.titleText
            color: "#e8edf5"
            width: Math.max(120, parent.width - 120)
            elide: Text.ElideRight
        }
    }
}
