# eBPF Event Parsing Fixes

## Summary of Changes

### 1. Network Event Parsing
- **Fixed**: Now uses `parseRawNetworkEvent()` from `event_parsers.go`
- **Structure**: Correctly matches the C struct in `network_monitor.c`
- **Validation**: Uses `binary.Read()` for proper binary parsing with struct alignment

### 2. DNS Event Parsing  
- **Fixed**: Now uses `parseRawDNSEvent()` from `event_parsers.go`
- **Structure**: Correctly matches the C struct in `dns_monitor.c` with 256-byte domain field
- **Validation**: Properly handles null-terminated strings

### 3. Packet Event Parsing
- **Fixed**: Now uses `parseRawPacketEvent()` from `event_parsers.go`
- **Structure**: Correctly includes TGID field matching the C struct
- **Validation**: Handles all fields including command and interface names

### 4. Protocol Event Parsing
- **Fixed**: Now uses `parseRawProtocolEvent()` from `event_parsers.go`
- **Structure**: Includes the `error_msg` field that was missing
- **Validation**: Properly parses all 128-byte error message field

## Key Improvements

1. **Eliminated Manual Offset Parsing**: All parsers now use the robust `binary.Read()` approach
2. **Proper Struct Alignment**: Uses the exact C struct definitions for correct field alignment
3. **Error Handling**: Uses consistent `ParserError` types with detailed context
4. **Type Safety**: Returns properly typed events matching `enhanced_collector.go` expectations
5. **Naming Convention**: All parsers return event types matching the `CategoryXXX` constants

## Integration with Correlation Engine

The events are now properly structured to work with the correlation engine:
- Consistent timestamp formats (`time.Time`)
- Proper field names matching expected interfaces
- Event types align with correlation rules
- Container ID and command fields preserved for correlation

## Testing Recommendations

1. Verify events flow correctly from kernel to userspace
2. Check that all fields are populated with correct values
3. Ensure correlation engine receives properly formatted events
4. Test with high event rates to verify parsing performance