package types

// InstallStrategy represents different installation methods
type InstallStrategy string

const (
	StrategyBinary     InstallStrategy = "binary"
	StrategyDocker     InstallStrategy = "docker"
	StrategyKubernetes InstallStrategy = "kubernetes"
)
