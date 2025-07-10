package rules

import (
	"github.com/falseyair/tapio/pkg/correlation"
)

// RegisterDefaultRules registers all default correlation rules to the registry
func RegisterDefaultRules(registry *correlation.RuleRegistry) error {
	// Register OOM prediction rule
	oomRule := NewOOMPredictionRule(DefaultOOMPredictionConfig())
	if err := registry.RegisterRule(oomRule); err != nil {
		return err
	}

	// Register memory leak detection rule
	memLeakRule := NewMemoryLeakRule(DefaultMemoryLeakConfig())
	if err := registry.RegisterRule(memLeakRule); err != nil {
		return err
	}

	// Register CPU throttling rule
	cpuThrottleRule := NewCPUThrottlingRule(DefaultCPUThrottlingConfig())
	if err := registry.RegisterRule(cpuThrottleRule); err != nil {
		return err
	}

	// Register crash loop detection rule
	crashLoopRule := NewCrashLoopRule(DefaultCrashLoopConfig())
	if err := registry.RegisterRule(crashLoopRule); err != nil {
		return err
	}

	// Register disk pressure rule
	diskPressureRule := NewDiskPressureRule(DefaultDiskPressureConfig())
	if err := registry.RegisterRule(diskPressureRule); err != nil {
		return err
	}

	// Register advanced correlation rules

	// Register ETCD cascade rule
	etcdRule := NewETCDCascadeRule(DefaultETCDCascadeConfig())
	if err := registry.RegisterRule(etcdRule); err != nil {
		return err
	}

	// Register certificate cascade rule
	certRule := NewCertificateCascadeRule(DefaultCertificateCascadeConfig())
	if err := registry.RegisterRule(certRule); err != nil {
		return err
	}

	// Register admission lockdown rule
	admissionRule := NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig())
	if err := registry.RegisterRule(admissionRule); err != nil {
		return err
	}

	// Register control plane dependency rule
	controlPlaneRule := NewControlPlaneDepsRule(DefaultControlPlaneDepsConfig())
	if err := registry.RegisterRule(controlPlaneRule); err != nil {
		return err
	}

	return nil
}
