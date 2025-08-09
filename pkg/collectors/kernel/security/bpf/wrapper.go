package bpf

import "github.com/cilium/ebpf"

// SecuritymonitorObjects is a wrapper for the generated objects
type SecuritymonitorObjects struct {
	*securitymonitorObjects
}

// LoadSecuritymonitorObjects is a wrapper for loading security monitor objects
func LoadSecuritymonitorObjects(obj *SecuritymonitorObjects, opts *ebpf.CollectionOptions) error {
	inner := &securitymonitorObjects{}
	err := loadSecuritymonitorObjects(inner, opts)
	if err != nil {
		return err
	}
	obj.securitymonitorObjects = inner
	return nil
}

// LoadSecuritymonitor is a wrapper for loading the collection spec
func LoadSecuritymonitor() (*ebpf.CollectionSpec, error) {
	return loadSecuritymonitor()
}
