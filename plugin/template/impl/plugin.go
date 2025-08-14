package impl

import (
	"voxora/lib/libvoxora/types"
	"voxora/lib/libvoxora/types/plugin_schema"
)

type PluginImpl struct {
	schemaVersion uint32
	version       uint32
	testThing     types.GOThing
}

func (p *PluginImpl) SchemaVersion() uint32 {
	return p.schemaVersion
}

func (p *PluginImpl) Version() uint32 {
	return p.version
}

func (p *PluginImpl) TestThing() types.GOThing {
	return p.testThing
}

func Init() plugin_schema.VoxoraPluginV1 {
	return &PluginImpl{
		schemaVersion: 1, // has to match the plugin interface (1 for VoxoraPluginV1)
		version:       1, // incremental numeric versioning, should start at 1
		testThing: types.GOThing{
			A: 0,
			B: 'a',
		},
	}
}
