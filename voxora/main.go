package main

import (
	"fmt"
	"os"

	"github.com/McMelonTV/Voxora/libvoxora"
	"github.com/McMelonTV/Voxora/voxora/internal/renderinfo"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

func main() {
	fmt.Printf("using libvoxora v" + libvoxora.Version())

	qt.NewQApplication(os.Args)

	engine := qml.NewQQmlApplicationEngine()
	renderSnapshot := renderinfo.Collect(qt.QGuiApplication_PlatformName())

	url := qt.QUrl_FromLocalFile("main.qml")

	model := qt.NewQAbstractListModel()

	model.OnRowCount(func(parent *qt.QModelIndex) int {
		return 1000
	})

	model.OnData(func(idx *qt.QModelIndex, role int) *qt.QVariant {
		if !idx.IsValid() {
			return qt.NewQVariant()
		}

		switch qt.ItemDataRole(role) {
		case qt.DisplayRole:
			return qt.NewQVariant14(fmt.Sprintf("this is row %d", idx.Row()))

		default:
			return qt.NewQVariant()
		}
	})

	engine.RootContext().SetContextProperty("myModel", model.QObject)
	engine.RootContext().SetContextProperty2("graphicsPlatform", qt.NewQVariant14(renderSnapshot.Platform))
	engine.RootContext().SetContextProperty2("graphicsApi", qt.NewQVariant14(renderSnapshot.API))
	engine.RootContext().SetContextProperty2("graphicsRenderer", qt.NewQVariant14(renderSnapshot.Renderer))

	engine.Load(url)

	qt.QApplication_Exec()
}
