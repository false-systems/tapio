package syscallerrors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// SyscallErrorEvent represents a syscall error captured by eBPF
type SyscallErrorEvent struct {
	TimestampNs uint64
	PID         uint32
	PPID        uint32
	TID         uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	SyscallNr   int32
	ErrorCode   int32
	Category    uint8
	_pad        [3]uint8
	Comm        [16]byte
	Path        [256]byte
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Arg1        uint64
	Arg2        uint64
	Arg3        uint64
	ErrorCount  uint32
	_pad2       uint32
}

// CollectorStats represents collector statistics
type CollectorStats struct {
	TotalErrors       uint64
	ENOSPCCount       uint64
	ENOMEMCount       uint64
	ECONNREFUSEDCount uint64
	EIOCount          uint64
	EventsSent        uint64
	EventsDropped     uint64
}

// Collector implements the syscall error collector
type Collector struct {
	name   string
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// eBPF state (platform-specific)
	ebpfState interface{}

	// Event processing
	eventChan chan *domain.CollectorEvent
	stopOnce  sync.Once

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	enospcErrors    metric.Int64Counter
	enomemErrors    metric.Int64Counter
	econnrefErrors  metric.Int64Counter
	emfileErrors    metric.Int64Counter
	edquotErrors    metric.Int64Counter
	eventsDropped   metric.Int64Counter

	// Configuration
	config *Config

	// Error tracking for rate limiting
	lastErrorLogTime  time.Time
	errorLogInterval  time.Duration
	consecutiveErrors int

	// Health tracking
	healthy     bool
	healthMutex sync.RWMutex
}

// Config holds collector configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories map[string]bool // Map for O(1) lookup
	RequireAllMetrics bool            // If true, fail startup when metrics can't be created
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:   8 * 1024 * 1024, // 8MB
		EventChannelSize: 10000,
		RateLimitMs:      100,
		EnabledCategories: map[string]bool{
			"file":    true,
			"network": true,
			"memory":  true,
		},
		RequireAllMetrics: false, // Default to graceful degradation
	}
}

// NewCollector creates a new syscall error collector
func NewCollector(logger *zap.Logger, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OpenTelemetry
	tracer := otel.Tracer("syscall-errors-collector")
	meter := otel.Meter("syscall-errors-collector")

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		"syscall_errors_events_processed_total",
		metric.WithDescription("Total syscall error events processed"),
	)
	if err != nil && config.RequireAllMetrics {
		cancel()
		return nil, fmt.Errorf("failed to create events counter: %w", err)
	}

	errorsTotal, err := meter.Int64Counter(
		"syscall_errors_collector_errors_total",
		metric.WithDescription("Total errors in syscall error collector"),
	)
	if err != nil && config.RequireAllMetrics {
		cancel()
		return nil, fmt.Errorf("failed to create errors counter: %w", err)
	}

	processingTime, err := meter.Float64Histogram(
		"syscall_errors_processing_duration_ms",
		metric.WithDescription("Processing duration for syscall errors in milliseconds"),
	)
	if err != nil && config.RequireAllMetrics {
		cancel()
		return nil, fmt.Errorf("failed to create processing time histogram: %w", err)
	}

	// Error-specific metrics
	enospcErrors, _ := meter.Int64Counter(
		"syscall_errors_enospc_total",
		metric.WithDescription("Total ENOSPC errors captured"),
	)

	enomemErrors, _ := meter.Int64Counter(
		"syscall_errors_enomem_total",
		metric.WithDescription("Total ENOMEM errors captured"),
	)

	econnrefErrors, _ := meter.Int64Counter(
		"syscall_errors_econnrefused_total",
		metric.WithDescription("Total ECONNREFUSED errors captured"),
	)

	emfileErrors, _ := meter.Int64Counter(
		"syscall_errors_emfile_total",
		metric.WithDescription("Total EMFILE errors captured"),
	)

	edquotErrors, _ := meter.Int64Counter(
		"syscall_errors_edquot_total",
		metric.WithDescription("Total EDQUOT errors captured"),
	)

	eventsDropped, _ := meter.Int64Counter(
		"syscall_errors_events_dropped_total",
		metric.WithDescription("Total events dropped due to channel overflow"),
	)

	c := &Collector{
		name:              "syscall-errors",
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		eventChan:         make(chan *domain.CollectorEvent, config.EventChannelSize),
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		enospcErrors:      enospcErrors,
		enomemErrors:      enomemErrors,
		econnrefErrors:    econnrefErrors,
		emfileErrors:      emfileErrors,
		edquotErrors:      edquotErrors,
		eventsDropped:     eventsDropped,
		config:            config,
		errorLogInterval:  time.Duration(config.RateLimitMs) * time.Millisecond,
		healthy:           true,
	}

	return c, nil
}

// Start begins collecting syscall errors
func (c *Collector) Start(ctx context.Context) error {
	c.logger.Info("Starting syscall error collector",
		zap.Int("ringBufferSize", c.config.RingBufferSize),
		zap.Int("eventChannelSize", c.config.EventChannelSize),
		zap.Any("enabledCategories", c.config.EnabledCategories),
	)

	// Start eBPF (platform-specific)
	if err := c.startEBPF(); err != nil {
		return fmt.Errorf("failed to start eBPF: %w", err)
	}

	// Start event processor
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.readEvents()
	}()

	c.logger.Info("Syscall error collector started successfully")
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	var err error
	c.stopOnce.Do(func() {
		c.logger.Info("Stopping syscall error collector")

		// Cancel context to stop goroutines
		c.cancel()

		// Stop eBPF (platform-specific)
		c.stopEBPF()

		// Wait for goroutines
		c.wg.Wait()

		// Close event channel
		close(c.eventChan)

		c.setHealthy(false)
		c.logger.Info("Syscall error collector stopped")
	})
	return err
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.eventChan
}

// IsHealthy returns the health status
func (c *Collector) IsHealthy() bool {
	c.healthMutex.RLock()
	defer c.healthMutex.RUnlock()
	return c.healthy
}

// setHealthy sets the health status
func (c *Collector) setHealthy(healthy bool) {
	c.healthMutex.Lock()
	defer c.healthMutex.Unlock()
	c.healthy = healthy
}

// GetStats retrieves collector statistics (platform-specific implementation)
func (c *Collector) GetStats() (*CollectorStats, error) {
	// Implemented in platform-specific files
	return c.getStatsImpl()
}

// convertToCollectorEvent converts eBPF event to domain event
func (c *Collector) convertToCollectorEvent(event *SyscallErrorEvent) *domain.CollectorEvent {
	_, span := c.tracer.Start(c.ctx, "convertToCollectorEvent")
	defer span.End()

	// Convert basic fields
	pid := int32(event.PID)
	comm := bytesToString(event.Comm[:])
	path := bytesToString(event.Path[:])

	// Map syscall number to name
	syscallName := getSyscallName(event.SyscallNr)

	// Map error code to name and severity
	errorName := getErrorName(event.ErrorCode)
	severity := getErrorSeverity(event.ErrorCode)

	// Extract network context if applicable
	var customData map[string]string
	if event.Category == 2 && event.SrcIP != 0 { // network category
		customData = map[string]string{
			"src_ip":   formatIP(event.SrcIP),
			"dst_ip":   formatIP(event.DstIP),
			"src_port": fmt.Sprintf("%d", event.SrcPort),
			"dst_port": fmt.Sprintf("%d", event.DstPort),
		}
	}

	// Create collector event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("syscall-error-%d-%d", event.PID, event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.EventTypeKernelSyscall,
		Source:    c.name,
		Severity:  mapSeverity(severity),
		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				EventType:    "syscall_error",
				PID:          pid,
				PPID:         int32(event.PPID),
				UID:          int32(event.UID),
				GID:          int32(event.GID),
				Command:      comm,
				CgroupID:     event.CgroupID,
				Syscall:      syscallName,
				ReturnCode:   event.ErrorCode,
				ErrorMessage: errorName,
			},
			Custom: customData,
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"error_count": fmt.Sprintf("%d", event.ErrorCount),
				"category":    getCategoryName(event.Category),
				"path":        path,
			},
		},
	}
}

// Helper functions
func bytesToString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func formatIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24))
}

func getCategoryName(category uint8) string {
	switch category {
	case 1:
		return "file"
	case 2:
		return "network"
	case 3:
		return "memory"
	case 4:
		return "process"
	default:
		return "unknown"
	}
}

func mapSeverity(severity string) domain.EventSeverity {
	switch severity {
	case "critical":
		return domain.EventSeverityCritical
	case "high":
		return domain.EventSeverityError
	case "medium":
		return domain.EventSeverityWarning
	default:
		return domain.EventSeverityInfo
	}
}

// Error name mapping
func getErrorName(code int32) string {
	errorNames := map[int32]string{
		-1:   "EPERM",
		-2:   "ENOENT",
		-3:   "ESRCH",
		-4:   "EINTR",
		-5:   "EIO",
		-6:   "ENXIO",
		-7:   "E2BIG",
		-8:   "ENOEXEC",
		-9:   "EBADF",
		-10:  "ECHILD",
		-11:  "EAGAIN",
		-12:  "ENOMEM",
		-13:  "EACCES",
		-14:  "EFAULT",
		-16:  "EBUSY",
		-17:  "EEXIST",
		-18:  "EXDEV",
		-19:  "ENODEV",
		-20:  "ENOTDIR",
		-21:  "EISDIR",
		-22:  "EINVAL",
		-23:  "ENFILE",
		-24:  "EMFILE",
		-25:  "ENOTTY",
		-26:  "ETXTBSY",
		-27:  "EFBIG",
		-28:  "ENOSPC",
		-29:  "ESPIPE",
		-30:  "EROFS",
		-31:  "EMLINK",
		-32:  "EPIPE",
		-33:  "EDOM",
		-34:  "ERANGE",
		-35:  "EDEADLOCK",
		-36:  "ENAMETOOLONG",
		-37:  "ENOLCK",
		-38:  "ENOSYS",
		-39:  "ENOTEMPTY",
		-40:  "ELOOP",
		-42:  "ENOMSG",
		-61:  "ENODATA",
		-62:  "ETIME",
		-63:  "ENOSR",
		-71:  "EPROTO",
		-74:  "EBADMSG",
		-75:  "EOVERFLOW",
		-84:  "EILSEQ",
		-88:  "ENOTSOCK",
		-89:  "EDESTADDRREQ",
		-90:  "EMSGSIZE",
		-91:  "EPROTOTYPE",
		-92:  "ENOPROTOOPT",
		-93:  "EPROTONOSUPPORT",
		-94:  "ESOCKTNOSUPPORT",
		-95:  "EOPNOTSUPP",
		-96:  "EPFNOSUPPORT",
		-97:  "EAFNOSUPPORT",
		-98:  "EADDRINUSE",
		-99:  "EADDRNOTAVAIL",
		-100: "ENETDOWN",
		-101: "ENETUNREACH",
		-102: "ENETRESET",
		-103: "ECONNABORTED",
		-104: "ECONNRESET",
		-105: "ENOBUFS",
		-106: "EISCONN",
		-107: "ENOTCONN",
		-108: "ESHUTDOWN",
		-109: "ETOOMANYREFS",
		-110: "ETIMEDOUT",
		-111: "ECONNREFUSED",
		-112: "EHOSTDOWN",
		-113: "EHOSTUNREACH",
		-114: "EALREADY",
		-115: "EINPROGRESS",
		-116: "ESTALE",
		-117: "EUCLEAN",
		-118: "ENOTNAM",
		-119: "ENAVAIL",
		-120: "EISNAM",
		-121: "EREMOTEIO",
		-122: "EDQUOT",
		-123: "ENOMEDIUM",
		-124: "EMEDIUMTYPE",
		-125: "ECANCELED",
		-126: "ENOKEY",
		-127: "EKEYEXPIRED",
		-128: "EKEYREVOKED",
		-129: "EKEYREJECTED",
		-130: "EOWNERDEAD",
		-131: "ENOTRECOVERABLE",
	}

	if name, ok := errorNames[code]; ok {
		return name
	}
	return fmt.Sprintf("ERROR_%d", code)
}

// Error severity classification
func getErrorSeverity(code int32) string {
	switch code {
	case -12, -28, -122, -24: // ENOMEM, ENOSPC, EDQUOT, EMFILE
		return "critical"
	case -5, -111, -110, -104: // EIO, ECONNREFUSED, ETIMEDOUT, ECONNRESET
		return "high"
	case -13, -1, -16: // EACCES, EPERM, EBUSY
		return "medium"
	default:
		return "low"
	}
}

// Syscall name mapping (partial list)
func getSyscallName(nr int32) string {
	syscallNames := map[int32]string{
		0:   "read",
		1:   "write",
		2:   "open",
		3:   "close",
		4:   "stat",
		5:   "fstat",
		6:   "lstat",
		7:   "poll",
		8:   "lseek",
		9:   "mmap",
		10:  "mprotect",
		11:  "munmap",
		12:  "brk",
		13:  "rt_sigaction",
		14:  "rt_sigprocmask",
		15:  "rt_sigreturn",
		16:  "ioctl",
		17:  "pread64",
		18:  "pwrite64",
		19:  "readv",
		20:  "writev",
		21:  "access",
		22:  "pipe",
		23:  "select",
		24:  "sched_yield",
		25:  "mremap",
		26:  "msync",
		27:  "mincore",
		28:  "madvise",
		29:  "shmget",
		30:  "shmat",
		31:  "shmctl",
		32:  "dup",
		33:  "dup2",
		34:  "pause",
		35:  "nanosleep",
		36:  "getitimer",
		37:  "alarm",
		38:  "setitimer",
		39:  "getpid",
		40:  "sendfile",
		41:  "socket",
		42:  "connect",
		43:  "accept",
		44:  "sendto",
		45:  "recvfrom",
		46:  "sendmsg",
		47:  "recvmsg",
		48:  "shutdown",
		49:  "bind",
		50:  "listen",
		51:  "getsockname",
		52:  "getpeername",
		53:  "socketpair",
		54:  "setsockopt",
		55:  "getsockopt",
		56:  "clone",
		57:  "fork",
		58:  "vfork",
		59:  "execve",
		60:  "exit",
		61:  "wait4",
		62:  "kill",
		63:  "uname",
		64:  "semget",
		65:  "semop",
		66:  "semctl",
		67:  "shmdt",
		68:  "msgget",
		69:  "msgsnd",
		70:  "msgrcv",
		71:  "msgctl",
		72:  "fcntl",
		73:  "flock",
		74:  "fsync",
		75:  "fdatasync",
		76:  "truncate",
		77:  "ftruncate",
		78:  "getdents",
		79:  "getcwd",
		80:  "chdir",
		81:  "fchdir",
		82:  "rename",
		83:  "mkdir",
		84:  "rmdir",
		85:  "creat",
		86:  "link",
		87:  "unlink",
		88:  "symlink",
		89:  "readlink",
		90:  "chmod",
		91:  "fchmod",
		92:  "chown",
		93:  "fchown",
		94:  "lchown",
		95:  "umask",
		96:  "gettimeofday",
		97:  "getrlimit",
		98:  "getrusage",
		99:  "sysinfo",
		100: "times",
		101: "ptrace",
		102: "getuid",
		103: "syslog",
		104: "getgid",
		105: "setuid",
		106: "setgid",
		107: "geteuid",
		108: "getegid",
		109: "setpgid",
		110: "getppid",
		111: "getpgrp",
		112: "setsid",
		113: "setreuid",
		114: "setregid",
		115: "getgroups",
		116: "setgroups",
		117: "setresuid",
		118: "getresuid",
		119: "setresgid",
		120: "getresgid",
		121: "getpgid",
		122: "setfsuid",
		123: "setfsgid",
		124: "getsid",
		125: "capget",
		126: "capset",
		127: "rt_sigpending",
		128: "rt_sigtimedwait",
		129: "rt_sigqueueinfo",
		130: "rt_sigsuspend",
		131: "sigaltstack",
		132: "utime",
		133: "mknod",
		134: "uselib",
		135: "personality",
		136: "ustat",
		137: "statfs",
		138: "fstatfs",
		139: "sysfs",
		140: "getpriority",
		141: "setpriority",
		142: "sched_setparam",
		143: "sched_getparam",
		144: "sched_setscheduler",
		145: "sched_getscheduler",
		146: "sched_get_priority_max",
		147: "sched_get_priority_min",
		148: "sched_rr_get_interval",
		149: "mlock",
		150: "munlock",
		151: "mlockall",
		152: "munlockall",
		153: "vhangup",
		154: "modify_ldt",
		155: "pivot_root",
		156: "_sysctl",
		157: "prctl",
		158: "arch_prctl",
		159: "adjtimex",
		160: "setrlimit",
		161: "chroot",
		162: "sync",
		163: "acct",
		164: "settimeofday",
		165: "mount",
		166: "umount2",
		167: "swapon",
		168: "swapoff",
		169: "reboot",
		170: "sethostname",
		171: "setdomainname",
		172: "iopl",
		173: "ioperm",
		174: "create_module",
		175: "init_module",
		176: "delete_module",
		177: "get_kernel_syms",
		178: "query_module",
		179: "quotactl",
		180: "nfsservctl",
		181: "getpmsg",
		182: "putpmsg",
		183: "afs_syscall",
		184: "tuxcall",
		185: "security",
		186: "gettid",
		187: "readahead",
		188: "setxattr",
		189: "lsetxattr",
		190: "fsetxattr",
		191: "getxattr",
		192: "lgetxattr",
		193: "fgetxattr",
		194: "listxattr",
		195: "llistxattr",
		196: "flistxattr",
		197: "removexattr",
		198: "lremovexattr",
		199: "fremovexattr",
		200: "tkill",
		201: "time",
		202: "futex",
		203: "sched_setaffinity",
		204: "sched_getaffinity",
		205: "set_thread_area",
		206: "io_setup",
		207: "io_destroy",
		208: "io_getevents",
		209: "io_submit",
		210: "io_cancel",
		211: "get_thread_area",
		212: "lookup_dcookie",
		213: "epoll_create",
		214: "epoll_ctl_old",
		215: "epoll_wait_old",
		216: "remap_file_pages",
		217: "getdents64",
		218: "set_tid_address",
		219: "restart_syscall",
		220: "semtimedop",
		221: "fadvise64",
		222: "timer_create",
		223: "timer_settime",
		224: "timer_gettime",
		225: "timer_getoverrun",
		226: "timer_delete",
		227: "clock_settime",
		228: "clock_gettime",
		229: "clock_getres",
		230: "clock_nanosleep",
		231: "exit_group",
		232: "epoll_wait",
		233: "epoll_ctl",
		234: "tgkill",
		235: "utimes",
		236: "vserver",
		237: "mbind",
		238: "set_mempolicy",
		239: "get_mempolicy",
		240: "mq_open",
		241: "mq_unlink",
		242: "mq_timedsend",
		243: "mq_timedreceive",
		244: "mq_notify",
		245: "mq_getsetattr",
		246: "kexec_load",
		247: "waitid",
		248: "add_key",
		249: "request_key",
		250: "keyctl",
		251: "ioprio_set",
		252: "ioprio_get",
		253: "inotify_init",
		254: "inotify_add_watch",
		255: "inotify_rm_watch",
		256: "migrate_pages",
		257: "openat",
		258: "mkdirat",
		259: "mknodat",
		260: "fchownat",
		261: "futimesat",
		262: "newfstatat",
		263: "unlinkat",
		264: "renameat",
		265: "linkat",
		266: "symlinkat",
		267: "readlinkat",
		268: "fchmodat",
		269: "faccessat",
		270: "pselect6",
		271: "ppoll",
		272: "unshare",
		273: "set_robust_list",
		274: "get_robust_list",
		275: "splice",
		276: "tee",
		277: "sync_file_range",
		278: "vmsplice",
		279: "move_pages",
		280: "utimensat",
		281: "epoll_pwait",
		282: "signalfd",
		283: "timerfd_create",
		284: "eventfd",
		285: "fallocate",
		286: "timerfd_settime",
		287: "timerfd_gettime",
		288: "accept4",
		289: "signalfd4",
		290: "eventfd2",
		291: "epoll_create1",
		292: "dup3",
		293: "pipe2",
		294: "inotify_init1",
		295: "preadv",
		296: "pwritev",
		297: "rt_tgsigqueueinfo",
		298: "perf_event_open",
		299: "recvmmsg",
		300: "fanotify_init",
		301: "fanotify_mark",
		302: "prlimit64",
		303: "name_to_handle_at",
		304: "open_by_handle_at",
		305: "clock_adjtime",
		306: "syncfs",
		307: "sendmmsg",
		308: "setns",
		309: "getcpu",
		310: "process_vm_readv",
		311: "process_vm_writev",
		312: "kcmp",
		313: "finit_module",
		314: "sched_setattr",
		315: "sched_getattr",
		316: "renameat2",
		317: "seccomp",
		318: "getrandom",
		319: "memfd_create",
		320: "kexec_file_load",
		321: "bpf",
		322: "execveat",
		323: "userfaultfd",
		324: "membarrier",
		325: "mlock2",
		326: "copy_file_range",
		327: "preadv2",
		328: "pwritev2",
		329: "pkey_mprotect",
		330: "pkey_alloc",
		331: "pkey_free",
		332: "statx",
		333: "io_pgetevents",
		334: "rseq",
	}

	if name, ok := syscallNames[nr]; ok {
		return name
	}
	return fmt.Sprintf("syscall_%d", nr)
}