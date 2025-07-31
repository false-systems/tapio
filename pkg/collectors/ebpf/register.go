package ebpf

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	registry.Register("ebpf", func(config map[string]interface{}) (collectors.Collector, error) {
		return NewCollector("ebpf")
	})
}
