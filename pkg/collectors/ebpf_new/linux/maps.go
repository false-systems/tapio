//go:build linux
// +build linux

package linux

import (
	"fmt"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
)

// mapManager implements core.MapManager for Linux
type mapManager struct {
	mu   sync.RWMutex
	maps map[string]*managedMap
}

type managedMap struct {
	spec   core.MapSpec
	ebpfMap *ebpf.Map
	handle core.Map
}

// NewMapManager creates a new Linux eBPF map manager
func NewMapManager() core.MapManager {
	return &mapManager{
		maps: make(map[string]*managedMap),
	}
}

// CreateMap implements core.MapManager
func (mm *mapManager) CreateMap(spec core.MapSpec) (core.Map, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if _, exists := mm.maps[spec.Name]; exists {
		return nil, fmt.Errorf("map %s already exists", spec.Name)
	}

	// Convert map type
	mapType, err := convertMapType(spec.Type)
	if err != nil {
		return nil, err
	}

	// Create eBPF map spec
	ebpfSpec := &ebpf.MapSpec{
		Name:       spec.Name,
		Type:       mapType,
		KeySize:    spec.KeySize,
		ValueSize:  spec.ValueSize,
		MaxEntries: spec.MaxEntries,
	}

	// Create the map
	ebpfMap, err := ebpf.NewMap(ebpfSpec)
	if err != nil {
		return nil, core.MapError{
			MapName:   spec.Name,
			Operation: "create",
			Cause:     err,
		}
	}

	// Create map handle
	handle := &mapHandle{
		ebpfMap: ebpfMap,
		spec:    spec,
	}

	mm.maps[spec.Name] = &managedMap{
		spec:    spec,
		ebpfMap: ebpfMap,
		handle:  handle,
	}

	return handle, nil
}

// GetMap implements core.MapManager
func (mm *mapManager) GetMap(name string) (core.Map, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	managed, exists := mm.maps[name]
	if !exists {
		return nil, fmt.Errorf("map %s not found", name)
	}

	return managed.handle, nil
}

// DeleteMap implements core.MapManager
func (mm *mapManager) DeleteMap(name string) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	managed, exists := mm.maps[name]
	if !exists {
		return fmt.Errorf("map %s not found", name)
	}

	if err := managed.handle.Close(); err != nil {
		return core.MapError{
			MapName:   name,
			Operation: "delete",
			Cause:     err,
		}
	}

	delete(mm.maps, name)
	return nil
}

// ListMaps implements core.MapManager
func (mm *mapManager) ListMaps() ([]core.MapInfo, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	infos := make([]core.MapInfo, 0, len(mm.maps))
	for _, managed := range mm.maps {
		info := core.MapInfo{
			Name:       managed.spec.Name,
			Type:       managed.spec.Type,
			KeySize:    managed.spec.KeySize,
			ValueSize:  managed.spec.ValueSize,
			MaxEntries: managed.spec.MaxEntries,
		}

		// Get current entries count if possible
		// This is a simplified implementation
		info.CurrentEntries = 0

		infos = append(infos, info)
	}

	return infos, nil
}

// mapHandle implements core.Map
type mapHandle struct {
	ebpfMap *ebpf.Map
	spec    core.MapSpec
	mu      sync.RWMutex
}

// Lookup implements core.Map
func (mh *mapHandle) Lookup(key []byte) ([]byte, error) {
	if uint32(len(key)) != mh.spec.KeySize {
		return nil, fmt.Errorf("invalid key size: expected %d, got %d", mh.spec.KeySize, len(key))
	}

	value := make([]byte, mh.spec.ValueSize)
	if err := mh.ebpfMap.Lookup(key, &value); err != nil {
		return nil, core.MapError{
			MapName:   mh.spec.Name,
			Operation: "lookup",
			Cause:     err,
		}
	}

	return value, nil
}

// Update implements core.Map
func (mh *mapHandle) Update(key, value []byte) error {
	if uint32(len(key)) != mh.spec.KeySize {
		return fmt.Errorf("invalid key size: expected %d, got %d", mh.spec.KeySize, len(key))
	}

	if uint32(len(value)) != mh.spec.ValueSize {
		return fmt.Errorf("invalid value size: expected %d, got %d", mh.spec.ValueSize, len(value))
	}

	if err := mh.ebpfMap.Update(key, value, ebpf.UpdateAny); err != nil {
		return core.MapError{
			MapName:   mh.spec.Name,
			Operation: "update",
			Cause:     err,
		}
	}

	return nil
}

// Delete implements core.Map
func (mh *mapHandle) Delete(key []byte) error {
	if uint32(len(key)) != mh.spec.KeySize {
		return fmt.Errorf("invalid key size: expected %d, got %d", mh.spec.KeySize, len(key))
	}

	if err := mh.ebpfMap.Delete(key); err != nil {
		return core.MapError{
			MapName:   mh.spec.Name,
			Operation: "delete",
			Cause:     err,
		}
	}

	return nil
}

// Iterate implements core.Map
func (mh *mapHandle) Iterate(fn func(key, value []byte) error) error {
	iter := mh.ebpfMap.Iterate()
	key := make([]byte, mh.spec.KeySize)
	value := make([]byte, mh.spec.ValueSize)

	for iter.Next(&key, &value) {
		if err := fn(key, value); err != nil {
			return err
		}
	}

	if err := iter.Err(); err != nil {
		return core.MapError{
			MapName:   mh.spec.Name,
			Operation: "iterate",
			Cause:     err,
		}
	}

	return nil
}

// Close implements core.Map
func (mh *mapHandle) Close() error {
	return mh.ebpfMap.Close()
}

// ringBufferReader implements core.RingBufferReader
type ringBufferReader struct {
	reader *ringbuf.Reader
	name   string
	mu     sync.Mutex
}

// NewRingBufferReader creates a new ring buffer reader for the given map
func NewRingBufferReader(ebpfMap *ebpf.Map, name string) (core.RingBufferReader, error) {
	reader, err := ringbuf.NewReader(ebpfMap)
	if err != nil {
		return nil, core.RingBufferError{
			Operation: "create reader",
			Cause:     err,
		}
	}

	return &ringBufferReader{
		reader: reader,
		name:   name,
	}, nil
}

// Read implements core.RingBufferReader
func (rbr *ringBufferReader) Read() ([]byte, error) {
	rbr.mu.Lock()
	defer rbr.mu.Unlock()

	record, err := rbr.reader.Read()
	if err != nil {
		return nil, core.RingBufferError{
			Operation: "read",
			Cause:     err,
		}
	}

	// Copy the data since the record's data is only valid until the next read
	data := make([]byte, len(record.RawSample))
	copy(data, record.RawSample)

	return data, nil
}

// ReadBatch implements core.RingBufferReader
func (rbr *ringBufferReader) ReadBatch(maxEvents int) ([][]byte, error) {
	rbr.mu.Lock()
	defer rbr.mu.Unlock()

	var events [][]byte
	for i := 0; i < maxEvents; i++ {
		record, err := rbr.reader.Read()
		if err != nil {
			if err == ringbuf.ErrClosed {
				break
			}
			// If we've read some events, return them
			if len(events) > 0 {
				return events, nil
			}
			return nil, core.RingBufferError{
				Operation: "read batch",
				Cause:     err,
			}
		}

		// Copy the data
		data := make([]byte, len(record.RawSample))
		copy(data, record.RawSample)
		events = append(events, data)
	}

	return events, nil
}

// Close implements core.RingBufferReader
func (rbr *ringBufferReader) Close() error {
	rbr.mu.Lock()
	defer rbr.mu.Unlock()

	if err := rbr.reader.Close(); err != nil {
		return core.RingBufferError{
			Operation: "close",
			Cause:     err,
		}
	}

	return nil
}

// Helper functions

func convertMapType(t core.MapType) (ebpf.MapType, error) {
	switch t {
	case core.MapTypeHash:
		return ebpf.Hash, nil
	case core.MapTypeArray:
		return ebpf.Array, nil
	case core.MapTypeProgArray:
		return ebpf.ProgramArray, nil
	case core.MapTypePerfEventArray:
		return ebpf.PerfEventArray, nil
	case core.MapTypePerCPUHash:
		return ebpf.PerCPUHash, nil
	case core.MapTypePerCPUArray:
		return ebpf.PerCPUArray, nil
	case core.MapTypeStackTrace:
		return ebpf.StackTrace, nil
	case core.MapTypeCgroupArray:
		return ebpf.CGroupArray, nil
	case core.MapTypeLRUHash:
		return ebpf.LRUHash, nil
	case core.MapTypeLRUPerCPUHash:
		return ebpf.LRUCPUHash, nil
	case core.MapTypeLPMTrie:
		return ebpf.LPMTrie, nil
	case core.MapTypeArrayOfMaps:
		return ebpf.ArrayOfMaps, nil
	case core.MapTypeHashOfMaps:
		return ebpf.HashOfMaps, nil
	case core.MapTypeRingBuf:
		return ebpf.RingBuf, nil
	default:
		return 0, fmt.Errorf("unsupported map type: %s", t)
	}
}