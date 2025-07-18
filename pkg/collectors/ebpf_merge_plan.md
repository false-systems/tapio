# eBPF Collectors Merge Plan

## Current Situation
- **ebpf**: Simple, currently used by CLI (1,523 lines)
- **ebpf_new**: Advanced features, better architecture (7,023 lines)
- Both are ~12 days old

## Best Features from Each

### From `ebpf` (Keep):
1. ✅ **Simple Monitor Interface** - Used by CLI for availability checks
   - `GetAvailabilityStatus()` 
   - `GetDetailedStatus()`
   - Clean public API exports

2. ✅ **Working CLI Integration** - Already integrated with:
   - doctor.go
   - diagnose.go  
   - sniff.go
   - opentelemetry.go
   - prometheus.go

3. ✅ **Good Documentation** - 4 markdown files

### From `ebpf_new` (Adopt):
1. ✅ **Ring Buffer Support** - Better performance
2. ✅ **Batch Processing** - Efficiency for high-volume events
3. ✅ **Map Management** - Proper eBPF map handling
4. ✅ **Standalone Tools** - collector & debug commands
5. ✅ **Benchmarks** - Performance testing
6. ✅ **Better Configuration** - More granular control

## Merge Strategy

### Option 1: Enhance Current ebpf (Recommended)
1. Keep the current `ebpf` package structure
2. Add features from `ebpf_new`:
   - Ring buffer support
   - Batch processing
   - Map management
3. Maintain backward compatibility for CLI
4. Delete `ebpf_new` after merge

### Option 2: Migrate to ebpf_new
1. Add Monitor interface to `ebpf_new`
2. Add GetAvailabilityStatus/GetDetailedStatus functions
3. Update all CLI imports
4. Delete old `ebpf`

### Option 3: Create Single Unified Package
1. Create new structure combining both
2. Import best code from each
3. Single cohesive implementation
4. Delete both old packages

## Recommended Approach: Option 1

```bash
# Step 1: Copy advanced features to current ebpf
cp pkg/collectors/ebpf_new/internal/stream.go pkg/collectors/ebpf/internal/
cp pkg/collectors/ebpf_new/linux/maps.go pkg/collectors/ebpf/linux/
cp pkg/collectors/ebpf_new/linux/parser.go pkg/collectors/ebpf/linux/

# Step 2: Update interfaces
# Add RingBufferReader, MapManager to core/interfaces.go

# Step 3: Test everything works
go test ./pkg/collectors/ebpf/...

# Step 4: Copy benchmarks
cp pkg/collectors/ebpf_new/benchmarks_test.go pkg/collectors/ebpf/

# Step 5: Remove ebpf_new
rm -rf pkg/collectors/ebpf_new
```

## Benefits
- No breaking changes for CLI
- Get all advanced features
- Single maintained package
- Clean architecture