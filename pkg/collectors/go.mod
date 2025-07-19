module github.com/yairfalse/tapio

go 1.21

require (
    github.com/spf13/cobra v1.7.0
    github.com/spf13/viper v1.16.0
    k8s.io/api v0.33.3
    k8s.io/apimachinery v0.33.3
    k8s.io/client-go v0.33.3
    go.opentelemetry.io/otel/sdk v1.37.0
)

replace github.com/yairfalse/tapio/pkg/domain => ./pkg/domain
replace github.com/yairfalse/tapio/pkg/collectors/ebpf => ./pkg/collectors/ebpf
replace github.com/yairfalse/tapio/pkg/collectors/k8s => ./pkg/collectors/k8s
replace github.com/yairfalse/tapio/pkg/collectors/systemd => ./pkg/collectors/systemd
replace github.com/yairfalse/tapio/pkg/collectors/journald => ./pkg/collectors/journald
