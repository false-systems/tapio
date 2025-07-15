module github.com/yairfalse/tapio/cmd/tapio-cli

go 1.21

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/viper v1.19.0
	github.com/yairfalse/tapio/pkg/client v0.0.0
	google.golang.org/grpc v1.67.0
)

replace github.com/yairfalse/tapio/pkg/client => ../../pkg/client