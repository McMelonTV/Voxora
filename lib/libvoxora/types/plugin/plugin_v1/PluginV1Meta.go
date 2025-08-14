package plugin_v1

type PluginV1Meta interface {
	// SchemaVersion returns the version of the Voxora plugin schema
	// this plugin implements (e.g., 1 for PluginV1Schema).
	SchemaVersion() uint32

	// Version returns the implementation version of the specific plugin.
	// This should be updated whenever the plugin's code changes.
	Version() uint32

	Name() string
}

type PluginV1MetaImpl struct {
	schemaVersion uint32
	version       uint32
	name          string
}

func (m *PluginV1MetaImpl) SchemaVersion() uint32 {
	return m.schemaVersion
}

func (m *PluginV1MetaImpl) Version() uint32 {
	return m.version
}

func (m *PluginV1MetaImpl) Name() string {
	return m.name
}

func NewPluginV1MetaImpl(schemaVersion uint32, version uint32, name string) *PluginV1MetaImpl {
	return &PluginV1MetaImpl{
		schemaVersion,
		version,
		name,
	}
}
