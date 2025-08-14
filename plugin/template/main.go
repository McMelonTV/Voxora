package main

import "C"
import (
	"voxora/plugin/template/impl"
)

var PluginSchema = impl.Init()

//export Meta
func Meta() interface{} {
	return *PluginSchema.Meta() // TODO: find out if this is supposed to be a pointer or not
}

//export Plugin
func Plugin() interface{} {
	return *PluginSchema.Plugin() // TODO: find out if this is supposed to be a pointer or not
}

func main() {}
