module github.com/yairfalse/tapio/pkg/collectors/ebpf

go 1.24.3

require (
	github.com/cilium/ebpf v0.18.0
	github.com/yairfalse/tapio/pkg/domain v0.0.0-20240101000000-000000000000
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

// Local replace for development
replace github.com/yairfalse/tapio/pkg/domain => ../../domain
