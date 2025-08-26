#!/bin/bash

# Collector Audit Script
# This script analyzes each collector for quality, standards, and functionality

COLLECTORS_DIR="/Users/yair/projects/tapio/pkg/collectors"
OUTPUT_FILE="COLLECTOR_AUDIT_REPORT.md"

echo "# Tapio Collectors Comprehensive Audit Report" > $OUTPUT_FILE
echo "Generated: $(date)" >> $OUTPUT_FILE
echo "" >> $OUTPUT_FILE

# Function to check if collector uses eBPF
check_ebpf() {
    local dir=$1
    if grep -r "cilium/ebpf\|bpf2go\|kprobe\|tracepoint" "$dir" --include="*.go" > /dev/null 2>&1; then
        echo "eBPF"
    elif [ -d "$dir/bpf" ] || [ -d "$dir/bpf_src" ]; then
        echo "eBPF"
    else
        echo "Non-eBPF"
    fi
}

# Function to check event type
check_event_type() {
    local dir=$1
    if grep -r "domain.CollectorEvent" "$dir" --include="*.go" > /dev/null 2>&1; then
        echo "CollectorEvent"
    elif grep -r "domain.UnifiedEvent" "$dir" --include="*.go" > /dev/null 2>&1; then
        echo "UnifiedEvent"
    else
        echo "Unknown/None"
    fi
}

# Function to check test coverage
check_tests() {
    local dir=$1
    local test_files=$(find "$dir" -name "*_test.go" 2>/dev/null | wc -l)
    echo "$test_files test files"
}

# Function to check README
check_readme() {
    local dir=$1
    if [ -f "$dir/README.md" ]; then
        echo "✓"
    else
        echo "✗"
    fi
}

# Function to check for TODOs/FIXMEs
check_todos() {
    local dir=$1
    local todos=$(grep -r "TODO\|FIXME\|XXX\|HACK" "$dir" --include="*.go" 2>/dev/null | wc -l)
    echo "$todos"
}

# Function to check interfaces
check_interfaces() {
    local dir=$1
    local interfaces=$(grep -r "interface{}" "$dir" --include="*.go" 2>/dev/null | wc -l)
    echo "$interfaces"
}

# Main audit loop
echo "## Summary Table" >> $OUTPUT_FILE
echo "" >> $OUTPUT_FILE
echo "| Collector | Type | Event | Tests | README | TODOs | interface{} | Status |" >> $OUTPUT_FILE
echo "|-----------|------|-------|-------|--------|-------|-------------|--------|" >> $OUTPUT_FILE

for dir in $COLLECTORS_DIR/*/; do
    if [ -f "$dir/collector.go" ]; then
        name=$(basename "$dir")
        type=$(check_ebpf "$dir")
        event=$(check_event_type "$dir")
        tests=$(check_tests "$dir")
        readme=$(check_readme "$dir")
        todos=$(check_todos "$dir")
        interfaces=$(check_interfaces "$dir")
        
        # Determine status
        status="✓"
        if [ "$todos" -gt 0 ] || [ "$interfaces" -gt 10 ] || [ "$readme" = "✗" ]; then
            status="⚠️"
        fi
        
        echo "| $name | $type | $event | $tests | $readme | $todos | $interfaces | $status |" >> $OUTPUT_FILE
    fi
done

echo "" >> $OUTPUT_FILE
echo "## Detailed Analysis" >> $OUTPUT_FILE
echo "" >> $OUTPUT_FILE

# Detailed analysis for each collector
for dir in $COLLECTORS_DIR/*/; do
    if [ -f "$dir/collector.go" ]; then
        name=$(basename "$dir")
        echo "### $name" >> $OUTPUT_FILE
        echo "" >> $OUTPUT_FILE
        
        # Check structure
        echo "**Structure:**" >> $OUTPUT_FILE
        if [ -f "$dir/collector.go" ]; then echo "- ✓ collector.go" >> $OUTPUT_FILE; fi
        if [ -f "$dir/config.go" ]; then echo "- ✓ config.go" >> $OUTPUT_FILE; fi
        if [ -f "$dir/types.go" ]; then echo "- ✓ types.go" >> $OUTPUT_FILE; fi
        if [ -d "$dir/bpf" ] || [ -d "$dir/bpf_src" ]; then echo "- ✓ eBPF components" >> $OUTPUT_FILE; fi
        echo "" >> $OUTPUT_FILE
        
        # Check main interfaces
        echo "**Implements:**" >> $OUTPUT_FILE
        if grep -q "func.*Start.*context.Context" "$dir/collector.go" 2>/dev/null; then
            echo "- ✓ Start(context.Context)" >> $OUTPUT_FILE
        fi
        if grep -q "func.*Stop" "$dir/collector.go" 2>/dev/null; then
            echo "- ✓ Stop()" >> $OUTPUT_FILE
        fi
        if grep -q "func.*IsHealthy" "$dir/collector.go" 2>/dev/null; then
            echo "- ✓ IsHealthy()" >> $OUTPUT_FILE
        fi
        if grep -q "func.*Events.*chan" "$dir/collector.go" 2>/dev/null; then
            echo "- ✓ Events() channel" >> $OUTPUT_FILE
        fi
        echo "" >> $OUTPUT_FILE
        
        # Check for violations
        echo "**Issues:**" >> $OUTPUT_FILE
        local issues=0
        
        # Check for panics
        local panics=$(grep -r "panic(" "$dir" --include="*.go" 2>/dev/null | grep -v "_test.go" | wc -l)
        if [ "$panics" -gt 0 ]; then
            echo "- ⚠️ $panics panic() calls found" >> $OUTPUT_FILE
            issues=$((issues + 1))
        fi
        
        # Check for ignored errors
        local ignored=$(grep -r "_ = " "$dir" --include="*.go" 2>/dev/null | grep -v "_test.go" | wc -l)
        if [ "$ignored" -gt 0 ]; then
            echo "- ⚠️ $ignored ignored errors" >> $OUTPUT_FILE
            issues=$((issues + 1))
        fi
        
        if [ "$issues" -eq 0 ]; then
            echo "- ✓ No major issues found" >> $OUTPUT_FILE
        fi
        
        echo "" >> $OUTPUT_FILE
        echo "---" >> $OUTPUT_FILE
        echo "" >> $OUTPUT_FILE
    fi
done

echo "Audit complete. Report saved to $OUTPUT_FILE"