module github.com/yairfalse/tapio/cmd/plugins/tapio-prometheus

go 1.21

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/viper v1.19.0
	github.com/yairfalse/tapio/pkg/exports v0.0.0
	github.com/prometheus/client_golang v1.20.4
)

replace github.com/yairfalse/tapio/pkg/exports => ../../../pkg/exports