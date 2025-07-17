module github.com/yairfalse/tapio/pkg/collectors/journald

go 1.24.3

require (
	github.com/coreos/go-systemd/v22 v22.5.0
	github.com/yairfalse/tapio/pkg/domain v0.0.0-20240101000000-000000000000
)

// Local replace for development
replace github.com/yairfalse/tapio/pkg/domain => ../../domain
