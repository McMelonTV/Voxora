package plugin_v1

type PluginV1Schema interface {
	Meta() *PluginV1MetaImpl
	Plugin() *PluginV1
}

type PluginV1SchemaImpl struct {
	meta   *PluginV1MetaImpl
	plugin *PluginV1
}

func (p *PluginV1SchemaImpl) Meta() *PluginV1MetaImpl {
	return p.meta
}

func (p *PluginV1SchemaImpl) Plugin() *PluginV1 {
	return p.plugin
}

func NewPluginV1SchemaImpl(meta *PluginV1MetaImpl, plugin *PluginV1) *PluginV1SchemaImpl {
	return &PluginV1SchemaImpl{
		meta,
		plugin,
	}
}
