import QtQuick
import QtQuick.Controls

Column {
    objectName: "libraryListPanelRoot"
    property var bridge
    property var libraryListEntries: []
    property bool dataSavingMode: false

    signal connectSpotifyRequested()
    signal clearStreamCacheRequested()
    signal dataSavingModeToggled()
    signal openCollectionRequested(string uri, string name)

    anchors.fill: parent
    spacing: 0

    Rectangle {
        width: parent.width
        color: "#161616"
        border.color: "#2b2b2b"
        border.width: 1
        implicitHeight: controlsColumn.implicitHeight + 20

        Column {
            id: controlsColumn
            anchors.fill: parent
            anchors.margins: 10
            spacing: 6

            Row {
                spacing: 8

                Button {
                    objectName: "connectSpotifyButton"
                    text: bridge && bridge.isBusy ? "Authenticating..." : "Connect Spotify"
                    enabled: bridge ? !bridge.isBusy : true
                    onClicked: connectSpotifyRequested()
                }

                Text {
                    text: bridge ? bridge.statusText : ""
                    color: "#9ad0ff"
                    wrapMode: Text.WrapAnywhere
                    width: Math.max(120, controlsColumn.width - 170)
                }
            }

            Text {
                text: bridge ? bridge.libraryStatus : ""
                color: "#9ad0ff"
                wrapMode: Text.WrapAnywhere
            }

            Button {
                objectName: "clearStreamCacheButton"
                text: "Clear Stream Cache"
                onClicked: clearStreamCacheRequested()
            }

            Row {
                spacing: 10

                Text {
                    text: "Data Saving"
                    color: "#8ea4c2"
                    verticalAlignment: Text.AlignVCenter
                }

                Button {
                    objectName: "dataSavingToggleButton"
                    text: dataSavingMode ? "On" : "Off"
                    onClicked: dataSavingModeToggled()
                }
            }
        }
    }

    ListView {
        width: parent.width
        height: Math.max(0, parent.height - (controlsColumn.implicitHeight + 20))
        clip: true
        model: libraryListEntries
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
                onClicked: openCollectionRequested(modelData.URI || "", modelData.Name || "Playlist")
            }
        }
    }
}
