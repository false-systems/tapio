module github.com/yairfalse/tapio/pkg/collectors/correlation

go 1.24.3

require (
	github.com/yairfalse/tapio/pkg/domain v0.0.0
	github.com/yairfalse/tapio/pkg/patternrecognition v0.0.0
)

replace github.com/yairfalse/tapio/pkg/domain => ../../domain

replace github.com/yairfalse/tapio/pkg/patternrecognition => ../../patternrecognition

replace github.com/yairfalse/tapio/pkg/collectors/ebpf => ../ebpf

replace github.com/yairfalse/tapio/pkg/collectors/k8s => ../k8s

replace github.com/yairfalse/tapio/pkg/collectors/systemd => ../systemd

replace github.com/yairfalse/tapio/pkg/collectors/journald => ../journald
