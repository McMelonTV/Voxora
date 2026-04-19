import QtQuick
import QtQuick.Controls

Rectangle {
    objectName: "playbackPanelRoot"
    property int currentTrackIndex: -1
    property int trackCount: 0
    property bool isPlaying: false
    property bool hasSource: false
    property real bufferedProgress: 0
    property real playbackProgress: 0
    property string leftTimeText: "0:00"
    property string rightTimeText: "0:00"
    property bool seekEnabledForCurrentSource: true

    signal seekPressed(real mouseX, real width)
    signal seekMoved(real mouseX, real width, int buttons)
    signal seekReleased()
    signal seekCanceled()
    signal previousRequested()
    signal playPauseRequested()
    signal nextRequested()

    width: parent ? parent.width : 0
    height: 122
    color: "#101724"
    border.color: "#25344f"
    border.width: 1

    Column {
        anchors.fill: parent
        anchors.margins: 8
        spacing: 8

        Rectangle {
            id: progressTrack
            width: parent.width
            height: 14
            radius: 7
            color: "#182232"

            Rectangle {
                width: parent.width * bufferedProgress
                height: parent.height
                radius: 7
                color: "#5f7391"
            }

            Rectangle {
                width: parent.width * playbackProgress
                height: parent.height
                radius: 7
                color: "#9ad0ff"
            }

            MouseArea {
                objectName: "playbackSeekMouseArea"
                anchors.fill: parent
                anchors.margins: -10
                onPressed: function(mouse) {
                    seekPressed(mouse.x, progressTrack.width)
                }
                onPositionChanged: function(mouse) {
                    seekMoved(mouse.x, progressTrack.width, mouse.buttons)
                }
                onReleased: seekReleased()
                onCanceled: seekCanceled()
                cursorShape: Qt.PointingHandCursor
            }
        }

        Row {
            width: parent.width

            Text {
                id: leftTime
                text: leftTimeText
                color: "#8ea4c2"
                font.pixelSize: 11
            }

            Item {
                width: Math.max(0, parent.width - leftTime.width - rightTime.width)
                height: 1
            }

            Text {
                id: rightTime
                text: rightTimeText
                color: "#8ea4c2"
                font.pixelSize: 11
            }
        }

        Item {
            width: parent.width
            height: 40

            Row {
                anchors.centerIn: parent
                spacing: 10

                Button {
                    objectName: "playbackPreviousButton"
                    width: 96
                    text: "Previous"
                    enabled: currentTrackIndex > 0
                    onClicked: previousRequested()
                }

                Button {
                    objectName: "playbackToggleButton"
                    width: 96
                    text: isPlaying ? "Pause" : "Play"
                    enabled: hasSource
                    onClicked: playPauseRequested()
                }

                Button {
                    objectName: "playbackNextButton"
                    width: 96
                    text: "Next"
                    enabled: currentTrackIndex >= 0 && currentTrackIndex < (trackCount - 1)
                    onClicked: nextRequested()
                }
            }
        }
    }
}
