package kernel

import (
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/registry"
)

func init() {
	registry.Register("kernel", func(config map[string]interface{}) (collectors.Collector, error) {
		return NewModularCollector("kernel")
	})
}
