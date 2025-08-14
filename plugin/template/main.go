package main

import (
	"voxora/lib/libvoxora/types/plugin/plugin_v1"
	"voxora/plugin/template/impl"
)

var PluginSchema = impl.Init()

func Meta() plugin_v1.PluginV1MetaImpl {
	return *PluginSchema.Meta()
}

func Plugin() plugin_v1.PluginV1 {
	return *PluginSchema.Plugin()
}

func main() {}
