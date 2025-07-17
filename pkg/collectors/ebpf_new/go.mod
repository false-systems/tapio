module github.com/yairfalse/tapio/pkg/collectors/ebpf_new

go 1.24.3

require (
	github.com/cilium/ebpf v0.16.0
	github.com/yairfalse/tapio/pkg/domain v0.0.0
)

require (
	golang.org/x/exp v0.0.0-20230224173230-c95f2b4c22f2 // indirect
	golang.org/x/sys v0.20.0 // indirect
)

replace github.com/yairfalse/tapio/pkg/domain => ../../domain
