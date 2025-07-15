module github.com/yairfalse/tapio/cmd/tapio-engine

go 1.21

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/viper v1.19.0
	github.com/yairfalse/tapio/pkg/engine v0.0.0
	github.com/yairfalse/tapio/pkg/correlation v0.0.0
	github.com/yairfalse/tapio/pkg/k8s v0.0.0
	github.com/yairfalse/tapio/pkg/grpc v0.0.0
	google.golang.org/grpc v1.67.0
	k8s.io/client-go v0.31.1
)

replace github.com/yairfalse/tapio/pkg/engine => ../../pkg/engine
replace github.com/yairfalse/tapio/pkg/correlation => ../../pkg/correlation
replace github.com/yairfalse/tapio/pkg/k8s => ../../pkg/k8s
replace github.com/yairfalse/tapio/pkg/grpc => ../../pkg/grpc