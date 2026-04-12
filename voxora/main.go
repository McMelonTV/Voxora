package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/McMelonTV/Voxora/libvoxora"
	"github.com/McMelonTV/Voxora/voxora/internal/authbridge"
	"github.com/McMelonTV/Voxora/voxora/internal/renderinfo"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
)

func androidLibraryDir() string {
	file, err := os.Open("/proc/self/maps")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		for _, marker := range []string{
			"/libplugins_platforms_qtforandroid_",
			"/libQt6Core_",
			"/libvoxora_",
		} {
			if idx := strings.Index(line, marker); idx != -1 {
				start := strings.LastIndex(line[:idx], " ")
				if start == -1 {
					start = 0
				}
				libPath := strings.TrimSpace(line[start:])
				if libPath != "" {
					return filepath.Dir(libPath)
				}
			}
		}
	}

	return ""
}

func main() {
	fmt.Print("using libvoxora v" + libvoxora.Version())

	runtime.LockOSThread()

	androidLibDir := ""
	if runtime.GOOS == "android" {
		if libDir := androidLibraryDir(); libDir != "" {
			androidLibDir = libDir
			_ = os.Setenv("QT_PLUGIN_PATH", libDir)
			_ = os.Setenv("QT_QPA_PLATFORM_PLUGIN_PATH", libDir)
			qt.QCoreApplication_SetLibraryPaths([]string{libDir})
		}
	}

	qt.NewQGuiApplication(os.Args)

	engine := qml.NewQQmlApplicationEngine()
	if runtime.GOOS == "android" {
		engine.AddImportPath(":/qt-project.org/imports")
		engine.AddImportPath("qrc:/qt-project.org/imports")
		if androidLibDir != "" {
			engine.AddPluginPath(androidLibDir)
		}
	}
	renderSnapshot := renderinfo.Snapshot{
		Platform: qt.QGuiApplication_PlatformName(),
		API:      "Unknown",
		Renderer: "Unknown",
	}
	if runtime.GOOS != "android" {
		renderSnapshot = renderinfo.Collect(renderSnapshot.Platform)
	}

	url := qt.QUrl_FromLocalFile("main.qml")
	if runtime.GOOS == "android" {
		url = qt.NewQUrl3("qrc:/main.qml")
	}

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

	spotifyBridge := authbridge.NewSpotifyBridge()
	defer spotifyBridge.Close()

	engine.RootContext().SetContextProperty("myModel", model.QObject)
	engine.RootContext().SetContextProperty2("graphicsPlatform", qt.NewQVariant14(renderSnapshot.Platform))
	engine.RootContext().SetContextProperty2("graphicsApi", qt.NewQVariant14(renderSnapshot.API))
	engine.RootContext().SetContextProperty2("graphicsRenderer", qt.NewQVariant14(renderSnapshot.Renderer))
	engine.RootContext().SetContextProperty("spotifyAuthBridge", spotifyBridge.Object().QObject)

	engine.Load(url)
	qt.QGuiApplication_Exec()
}
