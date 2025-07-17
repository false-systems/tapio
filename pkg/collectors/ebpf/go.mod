module github.com/yairfalse/tapio/pkg/collectors/ebpf

go 1.24.3

require (
	github.com/cilium/ebpf v0.12.3
	github.com/yairfalse/tapio/pkg/domain v0.0.0-20240101000000-000000000000
)

require (
	golang.org/x/exp v0.0.0-20230224173230-c95f2b4c22f2 // indirect
	golang.org/x/sys v0.15.0 // indirect
)

// Local replace for development
replace github.com/yairfalse/tapio/pkg/domain => ../../domain
