module github.com/yairfalse/tapio

go 1.24.0

toolchain go1.24.3

require (
	github.com/spf13/cobra v1.7.0
	github.com/spf13/viper v1.16.0
)

require (
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/spf13/afero v1.9.5 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yairfalse/tapio/pkg/domain => ./pkg/domain

replace github.com/yairfalse/tapio/pkg/collectors/ebpf => ./pkg/collectors/ebpf

replace github.com/yairfalse/tapio/pkg/collectors/k8s => ./pkg/collectors/k8s

replace github.com/yairfalse/tapio/pkg/collectors/systemd => ./pkg/collectors/systemd

replace github.com/yairfalse/tapio/pkg/collectors/journald => ./pkg/collectors/journald
