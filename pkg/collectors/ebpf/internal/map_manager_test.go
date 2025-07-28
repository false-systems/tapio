//go:build linux
// +build linux

package internal

import (
	"testing"

	"github.com/cilium/ebpf"
)

func TestMapManager_CreateMap(t *testing.T) {
	mm := NewMapManager()

	tests := []struct {
		name    string
		mapName string
		spec    *ebpf.MapSpec
		wantErr bool
		errMsg  string
	}{
		{
			name:    "create hash map",
			mapName: "test_hash",
			spec: &ebpf.MapSpec{
				Type:       ebpf.Hash,
				KeySize:    4,
				ValueSize:  8,
				MaxEntries: 100,
			},
			wantErr: false,
		},
		{
			name:    "create array map",
			mapName: "test_array",
			spec: &ebpf.MapSpec{
				Type:       ebpf.Array,
				KeySize:    4,
				ValueSize:  16,
				MaxEntries: 50,
			},
			wantErr: false,
		},
		{
			name:    "create duplicate map",
			mapName: "test_hash", // Already exists
			spec: &ebpf.MapSpec{
				Type:       ebpf.Hash,
				KeySize:    4,
				ValueSize:  8,
				MaxEntries: 100,
			},
			wantErr: true,
			errMsg:  "map test_hash already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mm.CreateMap(tt.mapName, tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("CreateMap() error = %v, want %v", err.Error(), tt.errMsg)
			}
		})
	}

	// Cleanup
	mm.CloseAll()
}

func TestMapManager_MapOperations(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create a test map
	mapName := "test_operations"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 10,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		t.Fatalf("Failed to create test map: %v", err)
	}

	// Test GetMap
	t.Run("GetMap", func(t *testing.T) {
		m, exists := mm.GetMap(mapName)
		if !exists {
			t.Error("GetMap() should find existing map")
		}
		if m == nil {
			t.Error("GetMap() returned nil map")
		}

		_, exists = mm.GetMap("non_existent")
		if exists {
			t.Error("GetMap() should not find non-existent map")
		}
	})

	// Test UpdateMapElement
	t.Run("UpdateMapElement", func(t *testing.T) {
		key := uint32(1)
		value := uint64(100)

		err := mm.UpdateMapElement(mapName, key, value)
		if err != nil {
			t.Errorf("UpdateMapElement() error = %v", err)
		}

		err = mm.UpdateMapElement("non_existent", key, value)
		if err == nil {
			t.Error("UpdateMapElement() should fail for non-existent map")
		}
	})

	// Test LookupMapElement
	t.Run("LookupMapElement", func(t *testing.T) {
		key := uint32(1)
		var value uint64

		err := mm.LookupMapElement(mapName, key, &value)
		if err != nil {
			t.Errorf("LookupMapElement() error = %v", err)
		}
		if value != 100 {
			t.Errorf("LookupMapElement() value = %v, want 100", value)
		}
	})

	// Test DeleteMapElement
	t.Run("DeleteMapElement", func(t *testing.T) {
		key := uint32(1)

		err := mm.DeleteMapElement(mapName, key)
		if err != nil {
			t.Errorf("DeleteMapElement() error = %v", err)
		}

		// Verify deletion
		var value uint64
		err = mm.LookupMapElement(mapName, key, &value)
		if err == nil {
			t.Error("LookupMapElement() should fail after deletion")
		}
	})
}

func TestMapManager_IterateMap(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create and populate a test map
	mapName := "test_iterate"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 5,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		t.Fatalf("Failed to create test map: %v", err)
	}

	// Add test data
	testData := map[uint32]uint64{
		1: 100,
		2: 200,
		3: 300,
	}

	for k, v := range testData {
		if err := mm.UpdateMapElement(mapName, k, v); err != nil {
			t.Fatalf("Failed to update element: %v", err)
		}
	}

	// Test iteration
	count := 0
	var key uint32
	var value uint64

	err := mm.IterateMap(mapName, &key, &value, func(k, v interface{}) error {
		count++
		key := k.(*uint32)
		value := v.(*uint64)

		if expectedValue, ok := testData[*key]; !ok || expectedValue != *value {
			t.Errorf("Unexpected key-value pair: %d-%d", *key, *value)
		}
		return nil
	})

	if err != nil {
		t.Errorf("IterateMap() error = %v", err)
	}
	if count != len(testData) {
		t.Errorf("IterateMap() visited %d elements, want %d", count, len(testData))
	}
}

func TestMapManager_MapInfo(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create a test map
	mapName := "test_info"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Array,
		KeySize:    4,
		ValueSize:  16,
		MaxEntries: 100,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		t.Fatalf("Failed to create test map: %v", err)
	}

	// Test GetMapInfo
	info, err := mm.GetMapInfo(mapName)
	if err != nil {
		t.Errorf("GetMapInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("GetMapInfo() returned nil")
	}

	// Verify info matches spec
	if info.Type != spec.Type {
		t.Errorf("GetMapInfo() Type = %v, want %v", info.Type, spec.Type)
	}
	if info.KeySize != spec.KeySize {
		t.Errorf("GetMapInfo() KeySize = %v, want %v", info.KeySize, spec.KeySize)
	}
	if info.ValueSize != spec.ValueSize {
		t.Errorf("GetMapInfo() ValueSize = %v, want %v", info.ValueSize, spec.ValueSize)
	}
	if info.MaxEntries != spec.MaxEntries {
		t.Errorf("GetMapInfo() MaxEntries = %v, want %v", info.MaxEntries, spec.MaxEntries)
	}
}

func TestMapManager_ListMaps(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create multiple maps
	mapNames := []string{"map1", "map2", "map3"}
	for i, name := range mapNames {
		spec := &ebpf.MapSpec{
			Type:       ebpf.Hash,
			KeySize:    4,
			ValueSize:  8,
			MaxEntries: uint32(10 * (i + 1)),
		}
		if err := mm.CreateMap(name, spec); err != nil {
			t.Fatalf("Failed to create map %s: %v", name, err)
		}
	}

	// Test ListMaps
	list := mm.ListMaps()
	if len(list) != len(mapNames) {
		t.Errorf("ListMaps() returned %d maps, want %d", len(list), len(mapNames))
	}

	// Verify all maps are in the list
	mapSet := make(map[string]bool)
	for _, name := range list {
		mapSet[name] = true
	}

	for _, expectedName := range mapNames {
		if !mapSet[expectedName] {
			t.Errorf("ListMaps() missing map %s", expectedName)
		}
	}
}

func TestMapManager_CloseMap(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create a test map
	mapName := "test_close"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 10,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		t.Fatalf("Failed to create test map: %v", err)
	}

	// Close the map
	err := mm.CloseMap(mapName)
	if err != nil {
		t.Errorf("CloseMap() error = %v", err)
	}

	// Verify map is removed
	_, exists := mm.GetMap(mapName)
	if exists {
		t.Error("GetMap() should not find closed map")
	}

	// Try to close non-existent map
	err = mm.CloseMap("non_existent")
	if err == nil {
		t.Error("CloseMap() should fail for non-existent map")
	}
}

func TestMapManager_GetMapStats(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create multiple maps with different types
	maps := []struct {
		name string
		spec *ebpf.MapSpec
	}{
		{
			name: "hash_map",
			spec: &ebpf.MapSpec{
				Type:       ebpf.Hash,
				KeySize:    4,
				ValueSize:  8,
				MaxEntries: 100,
			},
		},
		{
			name: "array_map",
			spec: &ebpf.MapSpec{
				Type:       ebpf.Array,
				KeySize:    4,
				ValueSize:  16,
				MaxEntries: 50,
			},
		},
	}

	for _, m := range maps {
		if err := mm.CreateMap(m.name, m.spec); err != nil {
			t.Fatalf("Failed to create map %s: %v", m.name, err)
		}
	}

	// Get stats
	stats := mm.GetMapStats()

	// Verify total maps
	if totalMaps, ok := stats["total_maps"].(int); !ok || totalMaps != len(maps) {
		t.Errorf("GetMapStats() total_maps = %v, want %d", stats["total_maps"], len(maps))
	}

	// Verify map details
	if mapDetails, ok := stats["maps"].(map[string]map[string]interface{}); ok {
		for _, m := range maps {
			if details, exists := mapDetails[m.name]; exists {
				// Verify type
				if mapType, ok := details["type"].(string); !ok || mapType != m.spec.Type.String() {
					t.Errorf("Map %s type = %v, want %v", m.name, mapType, m.spec.Type.String())
				}
				// Verify key size
				if keySize, ok := details["key_size"].(uint32); !ok || keySize != m.spec.KeySize {
					t.Errorf("Map %s key_size = %v, want %v", m.name, keySize, m.spec.KeySize)
				}
			} else {
				t.Errorf("GetMapStats() missing details for map %s", m.name)
			}
		}
	} else {
		t.Error("GetMapStats() maps field has wrong type")
	}
}

func TestMapManager_ThreadSafety(t *testing.T) {
	mm := NewMapManager()
	defer mm.CloseAll()

	// Create initial map
	mapName := "test_concurrent"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 1000,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		t.Fatalf("Failed to create test map: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool)
	errors := make(chan error, 100)

	// Writer goroutines
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := uint32(id*100 + j)
				value := uint64(key * 2)
				if err := mm.UpdateMapElement(mapName, key, value); err != nil {
					errors <- err
				}
			}
			done <- true
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 200; j++ {
				key := uint32(j)
				var value uint64
				mm.LookupMapElement(mapName, key, &value)
			}
			done <- true
		}()
	}

	// Stats reader
	go func() {
		for i := 0; i < 50; i++ {
			mm.GetMapStats()
			mm.ListMaps()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 16; i++ {
		<-done
	}

	// Check for errors
	select {
	case err := <-errors:
		t.Errorf("Concurrent operation failed: %v", err)
	default:
		// No errors
	}
}

// Benchmark tests
func BenchmarkMapManager_UpdateMapElement(b *testing.B) {
	mm := NewMapManager()
	defer mm.CloseAll()

	mapName := "bench_map"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 10000,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		b.Fatalf("Failed to create benchmark map: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := uint32(i % 10000)
			value := uint64(i)
			mm.UpdateMapElement(mapName, key, value)
			i++
		}
	})
}

func BenchmarkMapManager_LookupMapElement(b *testing.B) {
	mm := NewMapManager()
	defer mm.CloseAll()

	mapName := "bench_lookup"
	spec := &ebpf.MapSpec{
		Type:       ebpf.Hash,
		KeySize:    4,
		ValueSize:  8,
		MaxEntries: 10000,
	}

	if err := mm.CreateMap(mapName, spec); err != nil {
		b.Fatalf("Failed to create benchmark map: %v", err)
	}

	// Pre-populate map
	for i := 0; i < 10000; i++ {
		key := uint32(i)
		value := uint64(i * 2)
		mm.UpdateMapElement(mapName, key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		var value uint64
		for pb.Next() {
			key := uint32(i % 10000)
			mm.LookupMapElement(mapName, key, &value)
			i++
		}
	})
}
