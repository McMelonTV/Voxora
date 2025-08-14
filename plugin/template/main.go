package main

/*
#include <stdint.h>
#include "../../lib/libvoxora/types/libvoxora_types.h"
*/
import "C"
import (
	"unsafe"
	"voxora/lib/libvoxora/types"
	"voxora/plugin/template/impl"
)

var Plugin = impl.Init()

//export SchemaVersion
func SchemaVersion() C.uint32_t {
	return C.uint32_t(Plugin.PluginMeta().SchemaVersion())
}

//export Version
func Version() C.uint32_t {
	return C.uint32_t(Plugin.PluginMeta().Version())
}

//export Name
func Name() *C.char {
	return C.CString(Plugin.PluginMeta().Name())
}

//export TestThing
func TestThing() C.Thing {
	goThing := *Plugin.TestThing()
	thing := types.GOThingToCThing(goThing)
	cThing := *(*C.Thing)(unsafe.Pointer(&thing))
	return cThing
}

func main() {}
