package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"unsafe"
)

// BinaryReader provides high-performance binary reading with SIMD optimization
type BinaryReader struct {
	data    []byte   // Source data
	pos     int      // Current position
	limit   int      // Data limit
	scratch [16]byte // Scratch space for reads

	// Validation
	checksum       uint32 // Expected checksum
	enableChecksum bool   // Whether to validate checksum
	actualChecksum uint32 // Calculated checksum

	// Performance tracking
	bytesRead      int64 // Total bytes read
	readOperations int64 // Number of read operations

	// SIMD optimization
	simdEnabled   bool // Enable SIMD operations
	simdThreshold int  // Use SIMD for reads larger than threshold
}

// NewBinaryReader creates a new binary reader for the given data
func NewBinaryReader(data []byte) *BinaryReader {
	return &BinaryReader{
		data:           data,
		limit:          len(data),
		enableChecksum: true,
		simdEnabled:    true,
		simdThreshold:  32,
	}
}

// NewBinaryReaderWithLimit creates a reader with a specified limit
func NewBinaryReaderWithLimit(data []byte, limit int) *BinaryReader {
	if limit > len(data) {
		limit = len(data)
	}

	return &BinaryReader{
		data:           data,
		limit:          limit,
		enableChecksum: true,
		simdEnabled:    true,
		simdThreshold:  32,
	}
}

// Reset resets the reader to the beginning of the data
func (r *BinaryReader) Reset() {
	r.pos = 0
	r.actualChecksum = 0
	r.bytesRead = 0
	r.readOperations = 0
}

// Position returns the current read position
func (r *BinaryReader) Position() int {
	return r.pos
}

// Remaining returns the number of bytes remaining
func (r *BinaryReader) Remaining() int {
	return r.limit - r.pos
}

// Available returns whether there are bytes available to read
func (r *BinaryReader) Available() bool {
	return r.pos < r.limit
}

// SetPosition sets the read position
func (r *BinaryReader) SetPosition(pos int) error {
	if pos < 0 || pos > r.limit {
		return fmt.Errorf("invalid position: %d (limit: %d)", pos, r.limit)
	}
	r.pos = pos
	return nil
}

// ReadU8 reads a uint8 value
func (r *BinaryReader) ReadU8() (uint8, error) {
	if err := r.ensureAvailable(1); err != nil {
		return 0, err
	}

	value := r.data[r.pos]
	r.pos++

	if r.enableChecksum {
		r.updateChecksum([]byte{value})
	}

	r.readOperations++
	r.bytesRead++
	return value, nil
}

// ReadU16 reads a uint16 value in little-endian format
func (r *BinaryReader) ReadU16() (uint16, error) {
	if err := r.ensureAvailable(2); err != nil {
		return 0, err
	}

	value := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2

	if r.enableChecksum {
		r.updateChecksum(r.data[r.pos-2 : r.pos])
	}

	r.readOperations++
	r.bytesRead += 2
	return value, nil
}

// ReadU32 reads a uint32 value in little-endian format
func (r *BinaryReader) ReadU32() (uint32, error) {
	if err := r.ensureAvailable(4); err != nil {
		return 0, err
	}

	value := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4

	if r.enableChecksum {
		r.updateChecksum(r.data[r.pos-4 : r.pos])
	}

	r.readOperations++
	r.bytesRead += 4
	return value, nil
}

// ReadU64 reads a uint64 value in little-endian format
func (r *BinaryReader) ReadU64() (uint64, error) {
	if err := r.ensureAvailable(8); err != nil {
		return 0, err
	}

	value := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8

	if r.enableChecksum {
		r.updateChecksum(r.data[r.pos-8 : r.pos])
	}

	r.readOperations++
	r.bytesRead += 8
	return value, nil
}

// ReadI8 reads an int8 value
func (r *BinaryReader) ReadI8() (int8, error) {
	value, err := r.ReadU8()
	return int8(value), err
}

// ReadI16 reads an int16 value in little-endian format
func (r *BinaryReader) ReadI16() (int16, error) {
	value, err := r.ReadU16()
	return int16(value), err
}

// ReadI32 reads an int32 value in little-endian format
func (r *BinaryReader) ReadI32() (int32, error) {
	value, err := r.ReadU32()
	return int32(value), err
}

// ReadI64 reads an int64 value in little-endian format
func (r *BinaryReader) ReadI64() (int64, error) {
	value, err := r.ReadU64()
	return int64(value), err
}

// ReadF32 reads a float32 value in little-endian format
func (r *BinaryReader) ReadF32() (float32, error) {
	bits, err := r.ReadU32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(bits), nil
}

// ReadF64 reads a float64 value in little-endian format
func (r *BinaryReader) ReadF64() (float64, error) {
	bits, err := r.ReadU64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(bits), nil
}

// ReadBool reads a boolean value from a single byte
func (r *BinaryReader) ReadBool() (bool, error) {
	value, err := r.ReadU8()
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

// ReadBytes reads a byte slice with length prefix
func (r *BinaryReader) ReadBytes() ([]byte, error) {
	// Read length prefix
	length, err := r.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("failed to read bytes length: %w", err)
	}

	// Read data
	return r.ReadBytesFixed(int(length))
}

// ReadBytesFixed reads a fixed number of bytes
func (r *BinaryReader) ReadBytesFixed(length int) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}

	if err := r.ensureAvailable(length); err != nil {
		return nil, err
	}

	data := make([]byte, length)
	copy(data, r.data[r.pos:r.pos+length])
	r.pos += length

	if r.enableChecksum {
		r.updateChecksum(data)
	}

	r.readOperations++
	r.bytesRead += int64(length)
	return data, nil
}

// ReadBytesInPlace reads bytes without copying (zero-copy)
func (r *BinaryReader) ReadBytesInPlace(length int) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}

	if err := r.ensureAvailable(length); err != nil {
		return nil, err
	}

	data := r.data[r.pos : r.pos+length]
	r.pos += length

	if r.enableChecksum {
		r.updateChecksum(data)
	}

	r.readOperations++
	r.bytesRead += int64(length)
	return data, nil
}

// ReadString reads a string with length prefix (UTF-8 encoded)
func (r *BinaryReader) ReadString() (string, error) {
	// Read length prefix
	length, err := r.ReadU32()
	if err != nil {
		return "", fmt.Errorf("failed to read string length: %w", err)
	}

	// Read string data
	return r.ReadStringFixed(int(length))
}

// ReadStringFixed reads a fixed-length string
func (r *BinaryReader) ReadStringFixed(length int) (string, error) {
	if length == 0 {
		return "", nil
	}

	if err := r.ensureAvailable(length); err != nil {
		return "", err
	}

	// Use unsafe to create string without copying data
	data := r.data[r.pos : r.pos+length]
	r.pos += length

	if r.enableChecksum {
		r.updateChecksum(data)
	}

	str := unsafe.String(unsafe.SliceData(data), len(data))

	r.readOperations++
	r.bytesRead += int64(length)
	return str, nil
}

// ReadVarInt reads a variable-length integer using LEB128 decoding
func (r *BinaryReader) ReadVarInt() (uint64, error) {
	var result uint64
	var shift uint

	for {
		if shift >= 64 {
			return 0, fmt.Errorf("varint overflow")
		}

		b, err := r.ReadU8()
		if err != nil {
			return 0, fmt.Errorf("failed to read varint byte: %w", err)
		}

		result |= uint64(b&0x7F) << shift

		if (b & 0x80) == 0 {
			break
		}

		shift += 7
	}

	return result, nil
}

// ReadVarIntSigned reads a signed variable-length integer using zigzag decoding
func (r *BinaryReader) ReadVarIntSigned() (int64, error) {
	encoded, err := r.ReadVarInt()
	if err != nil {
		return 0, err
	}

	// Zigzag decoding: map unsigned back to signed
	return int64((encoded >> 1) ^ (-(encoded & 1))), nil
}

// ReadLengthPrefixedData reads data with a variable-length size prefix
func (r *BinaryReader) ReadLengthPrefixedData() ([]byte, error) {
	length, err := r.ReadVarInt()
	if err != nil {
		return nil, fmt.Errorf("failed to read data length: %w", err)
	}

	return r.ReadBytesFixed(int(length))
}

// ReadAligned reads data aligned to the specified boundary
func (r *BinaryReader) ReadAligned(length int, alignment int) ([]byte, error) {
	// Calculate padding to skip
	padding := (alignment - (r.pos % alignment)) % alignment

	// Skip padding bytes
	if padding > 0 {
		if err := r.Skip(padding); err != nil {
			return nil, fmt.Errorf("failed to skip padding: %w", err)
		}
	}

	// Read aligned data
	return r.ReadBytesFixed(length)
}

// ReadRemaining reads all remaining bytes
func (r *BinaryReader) ReadRemaining() []byte {
	remaining := r.limit - r.pos
	if remaining <= 0 {
		return nil
	}

	data := make([]byte, remaining)
	copy(data, r.data[r.pos:r.limit])
	r.pos = r.limit

	if r.enableChecksum {
		r.updateChecksum(data)
	}

	r.readOperations++
	r.bytesRead += int64(remaining)
	return data
}

// Skip skips the specified number of bytes
func (r *BinaryReader) Skip(count int) error {
	if err := r.ensureAvailable(count); err != nil {
		return err
	}

	if r.enableChecksum {
		r.updateChecksum(r.data[r.pos : r.pos+count])
	}

	r.pos += count
	r.readOperations++
	r.bytesRead += int64(count)
	return nil
}

// Peek looks ahead at the next bytes without advancing the position
func (r *BinaryReader) Peek(count int) ([]byte, error) {
	if err := r.ensureAvailable(count); err != nil {
		return nil, err
	}

	return r.data[r.pos : r.pos+count], nil
}

// ReadCompact reads data in compact format using decompression techniques
func (r *BinaryReader) ReadCompact(bitWidth int) (uint64, error) {
	if bitWidth <= 0 || bitWidth > 64 {
		return 0, fmt.Errorf("invalid bit width: %d", bitWidth)
	}

	// Read packed value from minimum required bytes
	bytes := (bitWidth + 7) / 8
	var value uint64

	for i := 0; i < bytes; i++ {
		b, err := r.ReadU8()
		if err != nil {
			return 0, err
		}
		value |= uint64(b) << (i * 8)
	}

	// Mask to keep only the required bits
	mask := (uint64(1) << bitWidth) - 1
	return value & mask, nil
}

// ReadDelta reads a delta-encoded value
func (r *BinaryReader) ReadDelta(previous uint64) (uint64, error) {
	delta, err := r.ReadVarIntSigned()
	if err != nil {
		return 0, err
	}

	return uint64(int64(previous) + delta), nil
}

// ReadRLE reads run-length encoded data
func (r *BinaryReader) ReadRLE() ([]byte, error) {
	// Read number of runs
	numRuns, err := r.ReadVarInt()
	if err != nil {
		return nil, fmt.Errorf("failed to read run count: %w", err)
	}

	if numRuns == 0 {
		return nil, nil
	}

	var result []byte

	// Read each run
	for i := uint64(0); i < numRuns; i++ {
		value, err := r.ReadU8()
		if err != nil {
			return nil, fmt.Errorf("failed to read run value %d: %w", i, err)
		}

		length, err := r.ReadVarInt()
		if err != nil {
			return nil, fmt.Errorf("failed to read run length %d: %w", i, err)
		}

		// Expand run
		for j := uint64(0); j < length; j++ {
			result = append(result, value)
		}
	}

	return result, nil
}

// ReadZigZag reads a zigzag-encoded signed integer
func (r *BinaryReader) ReadZigZag() (int64, error) {
	return r.ReadVarIntSigned()
}

// ReadBatch reads multiple values efficiently using SIMD when possible
func (r *BinaryReader) ReadBatch(count int, valueSize int) ([][]byte, error) {
	totalSize := count * valueSize
	if err := r.ensureAvailable(totalSize); err != nil {
		return nil, err
	}

	// Use SIMD optimization for large batches
	if r.simdEnabled && totalSize >= r.simdThreshold {
		return r.readBatchSIMD(count, valueSize)
	}

	// Standard batch reading
	results := make([][]byte, count)
	for i := 0; i < count; i++ {
		data, err := r.ReadBytesFixed(valueSize)
		if err != nil {
			return nil, fmt.Errorf("failed to read batch item %d: %w", i, err)
		}
		results[i] = data
	}

	return results, nil
}

// readBatchSIMD uses SIMD-like optimization for batch reading
func (r *BinaryReader) readBatchSIMD(count int, valueSize int) ([][]byte, error) {
	// This would use actual SIMD instructions in a real implementation
	// For now, we use an optimized loop with better cache locality

	results := make([][]byte, count)
	totalSize := count * valueSize

	// Read all data at once for better cache performance
	allData := r.data[r.pos : r.pos+totalSize]
	r.pos += totalSize

	// Split into individual values
	for i := 0; i < count; i++ {
		start := i * valueSize
		end := start + valueSize

		// Create copy for each value
		results[i] = make([]byte, valueSize)
		copy(results[i], allData[start:end])
	}

	if r.enableChecksum {
		r.updateChecksum(allData)
	}

	r.readOperations++
	r.bytesRead += int64(totalSize)
	return results, nil
}

// Validation and checksum methods

// GetChecksum returns the calculated checksum
func (r *BinaryReader) GetChecksum() uint32 {
	return r.actualChecksum
}

// SetChecksumEnabled enables or disables checksum calculation
func (r *BinaryReader) SetChecksumEnabled(enabled bool) {
	r.enableChecksum = enabled
	if !enabled {
		r.actualChecksum = 0
	}
}

// ValidateChecksum validates the calculated checksum against expected
func (r *BinaryReader) ValidateChecksum(expected uint32) error {
	if !r.enableChecksum {
		return fmt.Errorf("checksum validation disabled")
	}

	if r.actualChecksum != expected {
		return fmt.Errorf("checksum mismatch: expected %08x, got %08x", expected, r.actualChecksum)
	}

	return nil
}

// Private methods

func (r *BinaryReader) ensureAvailable(needed int) error {
	if r.pos+needed > r.limit {
		return fmt.Errorf("not enough data: need %d bytes, have %d", needed, r.limit-r.pos)
	}
	return nil
}

func (r *BinaryReader) updateChecksum(data []byte) {
	r.actualChecksum = crc32.Update(r.actualChecksum, crc32.IEEETable, data)
}

// Performance monitoring

// GetStats returns reader performance statistics
func (r *BinaryReader) GetStats() ReaderStats {
	return ReaderStats{
		ReadOps:     r.readOperations,
		BytesRead:   r.bytesRead,
		Position:    int64(r.pos),
		Remaining:   int64(r.Remaining()),
		Utilization: float64(r.pos) / float64(r.limit),
	}
}

type ReaderStats struct {
	ReadOps     int64
	BytesRead   int64
	Position    int64
	Remaining   int64
	Utilization float64
}

// Clone creates a copy of the reader at its current state
func (r *BinaryReader) Clone() *BinaryReader {
	clone := &BinaryReader{
		data:           r.data, // Share the same underlying data
		pos:            r.pos,
		limit:          r.limit,
		enableChecksum: r.enableChecksum,
		actualChecksum: r.actualChecksum,
		simdEnabled:    r.simdEnabled,
		simdThreshold:  r.simdThreshold,
		bytesRead:      r.bytesRead,
		readOperations: r.readOperations,
	}

	copy(clone.scratch[:], r.scratch[:])

	return clone
}

// SetSIMDEnabled enables or disables SIMD optimizations
func (r *BinaryReader) SetSIMDEnabled(enabled bool) {
	r.simdEnabled = enabled
}

// SetSIMDThreshold sets the threshold for SIMD optimization
func (r *BinaryReader) SetSIMDThreshold(threshold int) {
	r.simdThreshold = threshold
}

// Advanced reading methods

// ReadWithTimeout reads with a context timeout (for streaming scenarios)
func (r *BinaryReader) ReadWithValidation(validator func([]byte) error) ([]byte, error) {
	// Read length first
	length, err := r.ReadU32()
	if err != nil {
		return nil, err
	}

	// Read data
	data, err := r.ReadBytesFixed(int(length))
	if err != nil {
		return nil, err
	}

	// Validate data
	if validator != nil {
		if err := validator(data); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	return data, nil
}
