module github.com/yairfalse/tapio/pkg/integrations/core

go 1.24.3

require (
	github.com/yairfalse/tapio/pkg/domain v0.0.0-20240101000000-000000000000
)

// Local replace for development
replace github.com/yairfalse/tapio/pkg/domain => ../../domain