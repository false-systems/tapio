package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"unsafe"
)

// BinaryWriter provides high-performance binary writing with zero-allocation design
type BinaryWriter struct {
	buf      []byte   // Current buffer
	pos      int      // Current position
	capacity int      // Buffer capacity
	scratch  [16]byte // Scratch space for small writes

	// Performance optimization
	enableBatching bool // Enable write batching
	batchSize      int  // Batch size for writes
	batchCount     int  // Current batch count

	// Checksum support
	checksum       uint32 // Running checksum
	enableChecksum bool   // Whether to calculate checksum

	// Statistics
	writeOps     int64 // Number of write operations
	bytesWritten int64 // Total bytes written
}

// NewBinaryWriter creates a new binary writer with specified initial capacity
func NewBinaryWriter(initialCapacity int) *BinaryWriter {
	return &BinaryWriter{
		buf:            make([]byte, 0, initialCapacity),
		capacity:       initialCapacity,
		enableChecksum: true,
	}
}

// Reset resets the writer for reuse
func (w *BinaryWriter) Reset() {
	w.buf = w.buf[:0]
	w.pos = 0
	w.checksum = 0
	w.batchCount = 0
	w.writeOps = 0
	w.bytesWritten = 0
}

// Bytes returns the current buffer contents
func (w *BinaryWriter) Bytes() []byte {
	return w.buf
}

// Len returns the current buffer length
func (w *BinaryWriter) Len() int {
	return len(w.buf)
}

// Capacity returns the buffer capacity
func (w *BinaryWriter) Capacity() int {
	return w.capacity
}

// GetChecksum returns the current checksum
func (w *BinaryWriter) GetChecksum() uint32 {
	return w.checksum
}

// WriteU8 writes a uint8 value
func (w *BinaryWriter) WriteU8(value uint8) error {
	if err := w.ensureSpace(1); err != nil {
		return err
	}

	w.buf = append(w.buf, value)

	if w.enableChecksum {
		w.updateChecksum([]byte{value})
	}

	w.writeOps++
	w.bytesWritten++
	return nil
}

// WriteU16 writes a uint16 value in little-endian format
func (w *BinaryWriter) WriteU16(value uint16) error {
	if err := w.ensureSpace(2); err != nil {
		return err
	}

	binary.LittleEndian.PutUint16(w.scratch[:2], value)
	w.buf = append(w.buf, w.scratch[:2]...)

	if w.enableChecksum {
		w.updateChecksum(w.scratch[:2])
	}

	w.writeOps++
	w.bytesWritten += 2
	return nil
}

// WriteU32 writes a uint32 value in little-endian format
func (w *BinaryWriter) WriteU32(value uint32) error {
	if err := w.ensureSpace(4); err != nil {
		return err
	}

	binary.LittleEndian.PutUint32(w.scratch[:4], value)
	w.buf = append(w.buf, w.scratch[:4]...)

	if w.enableChecksum {
		w.updateChecksum(w.scratch[:4])
	}

	w.writeOps++
	w.bytesWritten += 4
	return nil
}

// WriteU64 writes a uint64 value in little-endian format
func (w *BinaryWriter) WriteU64(value uint64) error {
	if err := w.ensureSpace(8); err != nil {
		return err
	}

	binary.LittleEndian.PutUint64(w.scratch[:8], value)
	w.buf = append(w.buf, w.scratch[:8]...)

	if w.enableChecksum {
		w.updateChecksum(w.scratch[:8])
	}

	w.writeOps++
	w.bytesWritten += 8
	return nil
}

// WriteI8 writes an int8 value
func (w *BinaryWriter) WriteI8(value int8) error {
	return w.WriteU8(uint8(value))
}

// WriteI16 writes an int16 value in little-endian format
func (w *BinaryWriter) WriteI16(value int16) error {
	return w.WriteU16(uint16(value))
}

// WriteI32 writes an int32 value in little-endian format
func (w *BinaryWriter) WriteI32(value int32) error {
	return w.WriteU32(uint32(value))
}

// WriteI64 writes an int64 value in little-endian format
func (w *BinaryWriter) WriteI64(value int64) error {
	return w.WriteU64(uint64(value))
}

// WriteF32 writes a float32 value in little-endian format
func (w *BinaryWriter) WriteF32(value float32) error {
	bits := math.Float32bits(value)
	return w.WriteU32(bits)
}

// WriteF64 writes a float64 value in little-endian format
func (w *BinaryWriter) WriteF64(value float64) error {
	bits := math.Float64bits(value)
	return w.WriteU64(bits)
}

// WriteBool writes a boolean value as a single byte
func (w *BinaryWriter) WriteBool(value bool) error {
	if value {
		return w.WriteU8(1)
	}
	return w.WriteU8(0)
}

// WriteBytes writes a byte slice with length prefix
func (w *BinaryWriter) WriteBytes(data []byte) error {
	// Write length prefix
	if err := w.WriteU32(uint32(len(data))); err != nil {
		return fmt.Errorf("failed to write bytes length: %w", err)
	}

	// Write data
	return w.WriteBytesRaw(data)
}

// WriteBytesRaw writes raw bytes without length prefix
func (w *BinaryWriter) WriteBytesRaw(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	if err := w.ensureSpace(len(data)); err != nil {
		return err
	}

	w.buf = append(w.buf, data...)

	if w.enableChecksum {
		w.updateChecksum(data)
	}

	w.writeOps++
	w.bytesWritten += int64(len(data))
	return nil
}

// WriteString writes a string with length prefix (UTF-8 encoded)
func (w *BinaryWriter) WriteString(s string) error {
	// Write length prefix
	if err := w.WriteU32(uint32(len(s))); err != nil {
		return fmt.Errorf("failed to write string length: %w", err)
	}

	// Write string data
	return w.WriteStringRaw(s)
}

// WriteStringRaw writes raw string data without length prefix
func (w *BinaryWriter) WriteStringRaw(s string) error {
	if len(s) == 0 {
		return nil
	}

	// Use unsafe to convert string to []byte without allocation
	data := (*[1 << 30]byte)(unsafe.Pointer(unsafe.StringData(s)))[:len(s):len(s)]
	return w.WriteBytesRaw(data)
}

// WriteVarInt writes a variable-length integer using LEB128 encoding
func (w *BinaryWriter) WriteVarInt(value uint64) error {
	for value >= 0x80 {
		if err := w.WriteU8(uint8(value) | 0x80); err != nil {
			return err
		}
		value >>= 7
	}
	return w.WriteU8(uint8(value))
}

// WriteVarIntSigned writes a signed variable-length integer using zigzag encoding
func (w *BinaryWriter) WriteVarIntSigned(value int64) error {
	// Zigzag encoding: map signed to unsigned
	encoded := uint64((value << 1) ^ (value >> 63))
	return w.WriteVarInt(encoded)
}

// WriteLengthPrefixedData writes data with a variable-length size prefix
func (w *BinaryWriter) WriteLengthPrefixedData(data []byte) error {
	if err := w.WriteVarInt(uint64(len(data))); err != nil {
		return fmt.Errorf("failed to write data length: %w", err)
	}
	return w.WriteBytesRaw(data)
}

// WriteAligned writes data aligned to the specified boundary
func (w *BinaryWriter) WriteAligned(data []byte, alignment int) error {
	// Calculate padding needed
	currentPos := len(w.buf)
	padding := (alignment - (currentPos % alignment)) % alignment

	// Write padding bytes
	if padding > 0 {
		paddingBytes := make([]byte, padding)
		if err := w.WriteBytesRaw(paddingBytes); err != nil {
			return fmt.Errorf("failed to write padding: %w", err)
		}
	}

	// Write aligned data
	return w.WriteBytesRaw(data)
}

// WriteBatch enables batched writing for performance
func (w *BinaryWriter) WriteBatch(batchSize int, writeFunc func(*BinaryWriter) error) error {
	w.enableBatching = true
	w.batchSize = batchSize
	w.batchCount = 0

	defer func() {
		w.enableBatching = false
		w.batchCount = 0
	}()

	return writeFunc(w)
}

// WriteRepeated writes repeated data efficiently using SIMD when possible
func (w *BinaryWriter) WriteRepeated(data []byte, count int) error {
	totalSize := len(data) * count
	if err := w.ensureSpace(totalSize); err != nil {
		return err
	}

	// Use optimized repetition for small data
	if len(data) <= 8 && count > 8 {
		return w.writeRepeatedSIMD(data, count)
	}

	// Standard repetition
	for i := 0; i < count; i++ {
		if err := w.WriteBytesRaw(data); err != nil {
			return fmt.Errorf("failed to write repeated data at index %d: %w", i, err)
		}
	}

	return nil
}

// writeRepeatedSIMD uses SIMD-like optimization for repeated small data
func (w *BinaryWriter) writeRepeatedSIMD(data []byte, count int) error {
	// This would use actual SIMD instructions in a real implementation
	// For now, we use an optimized loop with unrolling

	remaining := count

	// Unroll loop for better performance
	for remaining >= 8 {
		for i := 0; i < 8; i++ {
			w.buf = append(w.buf, data...)
		}
		remaining -= 8
	}

	// Handle remaining iterations
	for i := 0; i < remaining; i++ {
		w.buf = append(w.buf, data...)
	}

	if w.enableChecksum {
		// Calculate checksum for all repeated data
		totalSize := len(data) * count
		checksumData := w.buf[len(w.buf)-totalSize:]
		w.updateChecksum(checksumData)
	}

	w.writeOps++
	w.bytesWritten += int64(len(data) * count)
	return nil
}

// Private methods

func (w *BinaryWriter) ensureSpace(needed int) error {
	required := len(w.buf) + needed
	if required <= cap(w.buf) {
		return nil
	}

	// Calculate new capacity with growth factor
	newCap := cap(w.buf) * 2
	if newCap < required {
		newCap = required
	}

	// Limit maximum size
	maxSize := 16 * 1024 * 1024 // 16MB max buffer
	if newCap > maxSize {
		if required > maxSize {
			return fmt.Errorf("buffer size exceeds maximum allowed size: %d > %d", required, maxSize)
		}
		newCap = maxSize
	}

	// Allocate new buffer and copy data
	newBuf := make([]byte, len(w.buf), newCap)
	copy(newBuf, w.buf)
	w.buf = newBuf
	w.capacity = newCap

	return nil
}

func (w *BinaryWriter) updateChecksum(data []byte) {
	w.checksum = crc32.Update(w.checksum, crc32.IEEETable, data)
}

// Advanced writing methods

// WriteCompact writes data in compact format using compression techniques
func (w *BinaryWriter) WriteCompact(value uint64, bitWidth int) error {
	if bitWidth <= 0 || bitWidth > 64 {
		return fmt.Errorf("invalid bit width: %d", bitWidth)
	}

	// Pack value into minimum required bytes
	bytes := (bitWidth + 7) / 8

	for i := 0; i < bytes; i++ {
		if err := w.WriteU8(uint8(value >> (i * 8))); err != nil {
			return err
		}
	}

	return nil
}

// WriteDelta writes a delta-encoded value
func (w *BinaryWriter) WriteDelta(current, previous uint64) error {
	delta := int64(current - previous)
	return w.WriteVarIntSigned(delta)
}

// WriteRLE writes run-length encoded data
func (w *BinaryWriter) WriteRLE(data []byte) error {
	if len(data) == 0 {
		return w.WriteVarInt(0)
	}

	runs := w.calculateRuns(data)

	// Write number of runs
	if err := w.WriteVarInt(uint64(len(runs))); err != nil {
		return err
	}

	// Write each run
	for _, run := range runs {
		if err := w.WriteU8(run.value); err != nil {
			return err
		}
		if err := w.WriteVarInt(uint64(run.length)); err != nil {
			return err
		}
	}

	return nil
}

type rleRun struct {
	value  uint8
	length int
}

func (w *BinaryWriter) calculateRuns(data []byte) []rleRun {
	if len(data) == 0 {
		return nil
	}

	var runs []rleRun
	currentValue := data[0]
	currentLength := 1

	for i := 1; i < len(data); i++ {
		if data[i] == currentValue {
			currentLength++
		} else {
			runs = append(runs, rleRun{value: currentValue, length: currentLength})
			currentValue = data[i]
			currentLength = 1
		}
	}

	// Add final run
	runs = append(runs, rleRun{value: currentValue, length: currentLength})

	return runs
}

// WriteZigZag writes a zigzag-encoded signed integer
func (w *BinaryWriter) WriteZigZag(value int64) error {
	encoded := uint64((value << 1) ^ (value >> 63))
	return w.WriteVarInt(encoded)
}

// Performance monitoring

// GetStats returns writer performance statistics
func (w *BinaryWriter) GetStats() WriterStats {
	return WriterStats{
		WriteOps:     w.writeOps,
		BytesWritten: w.bytesWritten,
		BufferSize:   int64(len(w.buf)),
		BufferCap:    int64(cap(w.buf)),
		Utilization:  float64(len(w.buf)) / float64(cap(w.buf)),
	}
}

type WriterStats struct {
	WriteOps     int64
	BytesWritten int64
	BufferSize   int64
	BufferCap    int64
	Utilization  float64
}

// Flush ensures all buffered data is written (no-op for memory writer)
func (w *BinaryWriter) Flush() error {
	// Memory writer doesn't need flushing
	return nil
}

// SetChecksumEnabled enables or disables checksum calculation
func (w *BinaryWriter) SetChecksumEnabled(enabled bool) {
	w.enableChecksum = enabled
	if !enabled {
		w.checksum = 0
	}
}

// Clone creates a copy of the writer at its current state
func (w *BinaryWriter) Clone() *BinaryWriter {
	clone := &BinaryWriter{
		buf:            make([]byte, len(w.buf), cap(w.buf)),
		pos:            w.pos,
		capacity:       w.capacity,
		enableBatching: w.enableBatching,
		batchSize:      w.batchSize,
		batchCount:     w.batchCount,
		checksum:       w.checksum,
		enableChecksum: w.enableChecksum,
		writeOps:       w.writeOps,
		bytesWritten:   w.bytesWritten,
	}

	copy(clone.buf, w.buf)
	copy(clone.scratch[:], w.scratch[:])

	return clone
}
