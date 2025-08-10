#!/bin/bash

# Fix remaining unsafe operations in test files
# This script replaces unsafe pointer operations with safe parsing methods

echo "Fixing unsafe operations in test files..."

# Function to fix a specific unsafe operation pattern
fix_unsafe_pattern() {
    local file="$1"
    local pattern="$2"
    local replacement="$3"
    
    if grep -q "$pattern" "$file"; then
        echo "Fixing pattern in $file"
        # Use sed to replace the pattern (simplified for common cases)
        sed -i.bak "$replacement" "$file"
    fi
}

# Fix kernel collector test file
KERNEL_TEST="/home/yair/projects/tapio/pkg/collectors/kernel/collector_test.go"

# Replace remaining NetworkInfo unsafe operations
sed -i.bak 's/*(\*NetworkInfo)(unsafe\.Pointer(\&buffer\[0\])) = netInfo/safeParser := collectors.NewSafeParser(); buffer, err := safeParser.MarshalStruct(netInfo); require.NoError(t, err)/g' "$KERNEL_TEST"

# Replace remaining FileInfo unsafe operations  
sed -i.bak 's/*(\*FileInfo)(unsafe\.Pointer(\&buffer\[0\])) = fileInfo/safeParser := collectors.NewSafeParser(); buffer, err := safeParser.MarshalStruct(fileInfo); require.NoError(t, err)/g' "$KERNEL_TEST"

# Replace remaining KernelEvent unsafe operations
sed -i.bak 's/*(\*KernelEvent)(unsafe\.Pointer(\&buffer\[0\])) = event/safeParser := collectors.NewSafeParser(); buffer, err := safeParser.MarshalStruct(event); require.NoError(t, err)/g' "$KERNEL_TEST"
sed -i.bak 's/*(\*KernelEvent)(unsafe\.Pointer(\&alignedBuffer\[0\])) = event/safeParser := collectors.NewSafeParser(); buffer, err := safeParser.MarshalStruct(event); require.NoError(t, err)/g' "$KERNEL_TEST"

# Fix cgroup test file
CGROUP_TEST="/home/yair/projects/tapio/pkg/collectors/kernel/cgroup_test.go"

# Replace the unsafe parsing in cgroup test
sed -i.bak 's/event := \*(\*KernelEvent)(unsafe\.Pointer(\&rawEvent\.Data\[0\]))/safeParser := collectors.NewSafeParser(); event, err := collectors.SafeCast[KernelEvent](safeParser, rawEvent.Data); require.NoError(t, err)/g' "$CGROUP_TEST"

echo "Unsafe operations fixed. Review the changes and test."
echo "Backup files created with .bak extension."