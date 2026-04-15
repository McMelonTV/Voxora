import QtQuick
import QtQuick.Controls
import QtMultimedia

Rectangle {
    id: root

    property real bufferedProgress: 0
    property real playbackProgress: 0
    property bool seekEnabledForCurrentSource: true
    property int positionMs: 0
    property int effectiveDurationMs: 0
    property int currentTrackIndex: -1
    property int trackCount: 0
    property var player

    signal playPreviousRequested()
    signal togglePlayPauseRequested()
    signal playNextRequested()
    signal scrubPressed(real mouseX, real trackWidth)
    signal scrubMoved(real mouseX, real trackWidth)
    signal scrubReleased()
    signal scrubCanceled()
    signal statusMessageRequested(string message)

    function formatMs(ms) {
        var totalSec = Math.max(0, Math.floor((ms || 0) / 1000))
        var m = Math.floor(totalSec / 60)
        var s = totalSec % 60
        return m + ":" + (s < 10 ? "0" + s : s)
    }

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
                width: parent.width * root.bufferedProgress
                height: parent.height
                radius: 7
                color: "#5f7391"
            }

            Rectangle {
                width: parent.width * root.playbackProgress
                height: parent.height
                radius: 7
                color: "#9ad0ff"
            }

            MouseArea {
                anchors.fill: parent
                anchors.margins: -10
                cursorShape: Qt.PointingHandCursor

                onPressed: function(mouse) {
                    if (!root.seekEnabledForCurrentSource) {
                        root.statusMessageRequested("Seek is available after stream buffering finishes")
                        return
                    }
                    root.scrubPressed(mouse.x, progressTrack.width)
                }

                onPositionChanged: function(mouse) {
                    if (mouse.buttons & Qt.LeftButton) {
                        root.scrubMoved(mouse.x, progressTrack.width)
                    }
                }

                onReleased: root.scrubReleased()
                onCanceled: root.scrubCanceled()
            }
        }

        Row {
            width: parent.width

            Text {
                id: leftTime
                text: root.formatMs(root.positionMs)
                color: "#8ea4c2"
                font.pixelSize: 11
            }

            Item {
                width: Math.max(0, parent.width - leftTime.width - rightTime.width)
                height: 1
            }

            Text {
                id: rightTime
                text: root.formatMs(root.effectiveDurationMs)
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
                    width: 96
                    text: "Previous"
                    enabled: root.currentTrackIndex > 0
                    onClicked: root.playPreviousRequested()
                }

                Button {
                    width: 96
                    text: root.player && root.player.playbackState === MediaPlayer.PlayingState ? "Pause" : "Play"
                    enabled: root.player && root.player.source && root.player.source.toString().length > 0
                    onClicked: root.togglePlayPauseRequested()
                }

                Button {
                    width: 96
                    text: "Next"
                    enabled: root.currentTrackIndex >= 0 && root.currentTrackIndex < (root.trackCount - 1)
                    onClicked: root.playNextRequested()
                }
            }
        }
    }
}
