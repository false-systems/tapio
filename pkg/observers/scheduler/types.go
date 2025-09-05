package scheduler

// SchedEvent represents a scheduling event from eBPF
type SchedEvent struct {
	TimestampNs uint64
	PID         uint32
	TID         uint32
	CPU         uint32
	EventType   uint32 // 1=delay, 2=throttle, 3=migration, 4=priority_inversion
	Value       uint64 // delay in ns, throttle time, etc.
	CgroupID    uint64
	Priority    int32
	NiceValue   int32
	Comm        [16]byte
	ContainerID [64]byte
	PrevCPU     uint32 // for migrations
	NextCPU     uint32 // for migrations
	RunTime     uint64 // nanoseconds of runtime
	WaitTime    uint64 // nanoseconds of wait time
}
