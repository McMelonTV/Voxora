package impl

import (
	"voxora/lib/libvoxora/types"
	"voxora/lib/libvoxora/types/plugin_schema"
)

type PluginMeta struct {
	schemaVersion uint32
	version       uint32
	name          string
}

func (m *PluginMeta) SchemaVersion() uint32 {
	return m.schemaVersion
}

func (m *PluginMeta) Version() uint32 {
	return m.version
}

func (m *PluginMeta) Name() string {
	return m.name
}

type PluginImpl struct {
	pluginMeta *PluginMeta
	testThing  *types.GOThing
}

func (p *PluginImpl) PluginMeta() plugin_schema.VoxoraPluginMetaV1 {
	return p.pluginMeta
}

func (p *PluginImpl) TestThing() *types.GOThing {
	return p.testThing
}

func Init() plugin_schema.VoxoraPluginV1 {
	return &PluginImpl{
		pluginMeta: &PluginMeta{
			schemaVersion: 1, // has to match the plugin interface (1 for VoxoraPluginV1)
			version:       1, // incremental numeric versioning, should start at 1
			name:          "Template",
		},
		testThing: &types.GOThing{
			A: 0,
			B: 'a',
		},
	}
}
