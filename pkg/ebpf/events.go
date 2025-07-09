//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"fmt"
	"math"
	"time"
	"unsafe"
)

// EventType represents the type of eBPF event
type EventType uint32

const (
	EventMemoryAlloc EventType = 1
	EventMemoryFree  EventType = 2
	EventOOMKill     EventType = 3
	EventProcessExit EventType = 4
)

// MemoryEvent represents a memory-related event from eBPF
type MemoryEvent struct {
	Timestamp    time.Time
	PID          uint32
	TID          uint32
	Size         uint64
	TotalMemory  uint64
	EventType    EventType
	Command      string
	InContainer  bool
	ContainerPID uint32
}

// rawMemoryEvent matches the C struct exactly
type rawMemoryEvent struct {
	Timestamp    uint64
	PID          uint32
	TID          uint32
	Size         uint64
	TotalMemory  uint64
	EventType    uint32
	Command      [16]int8
	InContainer  uint8
	ContainerPID uint32
}

// parseRawMemoryEvent converts raw C struct to Go struct
func parseRawMemoryEvent(raw []byte) (*MemoryEvent, error) {
	if len(raw) < int(unsafe.Sizeof(rawMemoryEvent{})) {
		return nil, fmt.Errorf("raw event too small")
	}

	// Cast raw bytes to struct
	rawEvent := (*rawMemoryEvent)(unsafe.Pointer(&raw[0]))

	// Convert to Go struct
	event := &MemoryEvent{
		Timestamp:    time.Unix(0, int64(rawEvent.Timestamp)),
		PID:          rawEvent.PID,
		TID:          rawEvent.TID,
		Size:         rawEvent.Size,
		TotalMemory:  rawEvent.TotalMemory,
		EventType:    EventType(rawEvent.EventType),
		InContainer:  rawEvent.InContainer == 1,
		ContainerPID: rawEvent.ContainerPID,
	}

	// Convert C string to Go string
	command := (*[16]byte)(unsafe.Pointer(&rawEvent.Command[0]))
	event.Command = string(command[:clen(command[:])])

	return event, nil
}

// clen returns the length of a null-terminated C string
func clen(b []byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return i
		}
	}
	return len(b)
}

// PredictOOM calculates if and when a process will hit OOM
func (stats *ProcessMemoryStats) PredictOOM(memoryLimit uint64) *OOMPrediction {
	if len(stats.GrowthPattern) < 2 {
		return nil
	}

	// Calculate growth rate over last few minutes
	recent := stats.GrowthPattern
	if len(recent) > 10 {
		recent = recent[len(recent)-10:] // Last 10 data points
	}

	if len(recent) < 2 {
		return nil
	}

	// Linear regression to find growth rate
	totalTime := recent[len(recent)-1].Timestamp.Sub(recent[0].Timestamp).Seconds()
	totalGrowth := int64(recent[len(recent)-1].Usage) - int64(recent[0].Usage)

	if totalTime <= 0 || totalGrowth <= 0 {
		return nil // No growth or negative growth
	}

	growthRate := float64(totalGrowth) / totalTime // bytes per second
	currentUsage := stats.CurrentUsage

	if currentUsage >= memoryLimit {
		return &OOMPrediction{
			PID:                stats.PID,
			TimeToOOM:          0,
			Confidence:         0.95,
			CurrentUsage:       currentUsage,
			MemoryLimit:        memoryLimit,
			PredictedPeakUsage: currentUsage,
		}
	}

	remainingMemory := memoryLimit - currentUsage
	timeToOOM := time.Duration(float64(remainingMemory)/growthRate) * time.Second

	// Calculate confidence based on consistency of growth
	confidence := calculateGrowthConfidence(recent)

	return &OOMPrediction{
		PID:                stats.PID,
		TimeToOOM:          timeToOOM,
		Confidence:         confidence,
		CurrentUsage:       currentUsage,
		MemoryLimit:        memoryLimit,
		PredictedPeakUsage: currentUsage + uint64(growthRate*timeToOOM.Seconds()),
	}
}

// calculateGrowthConfidence determines how confident we are in the growth pattern
func calculateGrowthConfidence(points []MemoryDataPoint) float64 {
	if len(points) < 3 {
		return 0.5
	}

	// Calculate variance in growth rates between consecutive points
	var rates []float64
	for i := 1; i < len(points); i++ {
		timeDiff := points[i].Timestamp.Sub(points[i-1].Timestamp).Seconds()
		if timeDiff > 0 {
			usageDiff := int64(points[i].Usage) - int64(points[i-1].Usage)
			rate := float64(usageDiff) / timeDiff
			rates = append(rates, rate)
		}
	}

	if len(rates) < 2 {
		return 0.5
	}

	// Calculate standard deviation of rates
	var sum, sumSq float64
	for _, rate := range rates {
		sum += rate
		sumSq += rate * rate
	}

	mean := sum / float64(len(rates))
	variance := (sumSq / float64(len(rates))) - (mean * mean)
	stdDev := math.Sqrt(variance)

	// Lower standard deviation = higher confidence
	// Normalize to 0.0-1.0 range
	confidence := 1.0 / (1.0 + stdDev/mean)

	// Clamp to reasonable range
	if confidence < 0.5 {
		confidence = 0.5
	}
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}
