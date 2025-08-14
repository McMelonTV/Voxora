package impl

import (
	"voxora/lib/libvoxora/types/plugin/plugin_v1"
)

type PluginImpl struct{}

func Init() *plugin_v1.PluginV1SchemaImpl {
	meta := plugin_v1.NewPluginV1MetaImpl(1, 1, "Template")
	plugin := plugin_v1.NewPluginV1(&PluginImpl{})

	return plugin_v1.NewPluginV1SchemaImpl(meta, plugin)
}
