package plugin_schema

import "voxora/lib/libvoxora/types"

type VoxoraPluginMetaV1 interface {
	// SchemaVersion returns the version of the Voxora plugin schema
	// this plugin implements (e.g., 1 for VoxoraPluginV1).
	SchemaVersion() uint32

	// Version returns the implementation version of the specific plugin.
	// This should be updated whenever the plugin's code changes.
	Version() uint32

	Name() string // TODO: make sure the *C.char always gets freed on unload
}

type VoxoraPluginV1 interface {
	PluginMeta() VoxoraPluginMetaV1
	TestThing() *types.GOThing
}
