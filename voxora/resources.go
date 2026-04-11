package main

//go:generate miqt-rcc -Input "resources.qrc" -OutputGo "resources.go" -OutputRcc "resources.rcc" -Qt6

import (
	"embed"

	qt "github.com/mappu/miqt/qt6"
)

//go:embed resources.rcc
var _resourceRcc []byte

func init() {
	_ = embed.FS{}
	qt.QResource_RegisterResourceWithRccData(&_resourceRcc[0])
}
