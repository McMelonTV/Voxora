import QtQuick
import QtQuick.Controls

Column {
    id: root

    property var bridge
    property var libraryListEntries: []
    property real userVolume: 0.8
    property bool dataSavingMode: false

    signal connectSpotifyRequested()
    signal clearStreamCacheRequested()
    signal volumeChangedByUser(real value)
    signal dataSavingToggleRequested()
    signal collectionRequested(string uri, string name)
    signal searchRequested(string query)

    spacing: 0

    function trimmedSearchQuery() {
        return searchField.text ? searchField.text.trim() : ""
    }

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
                    text: root.bridge && root.bridge.isBusy ? "Authenticating..." : "Connect Spotify"
                    enabled: !(root.bridge && root.bridge.isBusy)
                    onClicked: root.connectSpotifyRequested()
                }

                Text {
                    text: root.bridge ? root.bridge.statusText : ""
                    color: "#9ad0ff"
                    wrapMode: Text.WrapAnywhere
                    width: Math.max(120, controlsColumn.width - 170)
                }
            }

            Row {
                spacing: 8

                TextField {
                    id: searchField
                    width: Math.max(180, controlsColumn.width - searchButton.width - 18)
                    placeholderText: "Search songs on Spotify"
                    enabled: !(root.bridge && root.bridge.isBusy)
                    onAccepted: {
                        var query = root.trimmedSearchQuery()
                        if (query.length > 0) {
                            root.searchRequested(query)
                        }
                    }
                }

                Button {
                    id: searchButton
                    text: "Search"
                    enabled: !(root.bridge && root.bridge.isBusy) && root.trimmedSearchQuery().length > 0
                    onClicked: root.searchRequested(root.trimmedSearchQuery())
                }
            }

            Text {
                text: root.bridge ? root.bridge.libraryStatus : ""
                color: "#9ad0ff"
                wrapMode: Text.WrapAnywhere
            }

            Row {
                spacing: 10

                Text {
                    text: "Volume"
                    color: "#8ea4c2"
                    verticalAlignment: Text.AlignVCenter
                }

                Slider {
                    width: 160
                    from: 0
                    to: 1
                    value: root.userVolume
                    onValueChanged: root.volumeChangedByUser(value)
                }

                Button {
                    text: "Clear Stream Cache"
                    onClicked: root.clearStreamCacheRequested()
                }
            }

            Row {
                spacing: 10

                Text {
                    text: "Data Saving"
                    color: "#8ea4c2"
                    verticalAlignment: Text.AlignVCenter
                }

                Button {
                    text: root.dataSavingMode ? "On" : "Off"
                    onClicked: root.dataSavingToggleRequested()
                }
            }
        }
    }

    ListView {
        width: parent.width
        height: Math.max(0, parent.height - (controlsColumn.implicitHeight + 20))
        clip: true
        model: root.libraryListEntries

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
                onClicked: root.collectionRequested(modelData.URI || "", modelData.Name || "Playlist")
            }
        }
    }
}
