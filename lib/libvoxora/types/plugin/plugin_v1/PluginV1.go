package plugin_v1

type PluginV1 interface {
	A() uint32
	B() byte
}

func NewPluginV1(plugin PluginV1) *PluginV1 {
	return &plugin
}
