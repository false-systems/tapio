//! eBPF event structs mirroring the C definitions in ebpf/*.c
//!
//! These MUST match the C layouts exactly — a mismatch silently corrupts data.
//! Size assertions in tests enforce this at compile time.

// Address families (matching kernel AF_INET / AF_INET6)
pub const AF_INET: u16 = 2;
pub const AF_INET6: u16 = 10;

// Network event types (matching network_monitor.c defines)
pub const NET_EVENT_STATE_CHANGE: u8 = 0;
pub const NET_EVENT_RST_RECEIVED: u8 = 1;
pub const NET_EVENT_RETRANSMIT: u8 = 2;
pub const NET_EVENT_RTT_SPIKE: u8 = 3;

// Container event types (matching container_monitor.c defines)
pub const CONTAINER_EVENT_OOM: u32 = 0;
pub const CONTAINER_EVENT_EXIT: u32 = 1;

// TCP states (subset used by TAPIO — kernel has additional states like CLOSING, NEW_SYN_RECV)
// u16 to match NetworkEvent.old_state/new_state field width
pub const TCP_ESTABLISHED: u16 = 1;
pub const TCP_SYN_SENT: u16 = 2;
pub const TCP_SYN_RECV: u16 = 3;
pub const TCP_FIN_WAIT1: u16 = 4;
pub const TCP_FIN_WAIT2: u16 = 5;
pub const TCP_TIME_WAIT: u16 = 6;
pub const TCP_CLOSE: u16 = 7;
pub const TCP_CLOSE_WAIT: u16 = 8;
pub const TCP_LAST_ACK: u16 = 9;
pub const TCP_LISTEN: u16 = 10;

// Storage operation types (matching storage_monitor.c defines)
pub const OP_READ: u8 = 0;
pub const OP_WRITE: u8 = 1;

// Storage severity levels (matching storage_monitor.c defines)
pub const STORAGE_SEVERITY_NORMAL: u8 = 0;
pub const STORAGE_SEVERITY_WARNING: u8 = 1;
pub const STORAGE_SEVERITY_CRITICAL: u8 = 2;

pub const TAPIO_CONFIG_ABI_VERSION: u32 = 1;
pub const TAPIO_F_NETWORK: u64 = 1 << 0;
pub const TAPIO_F_STORAGE: u64 = 1 << 1;
pub const TAPIO_F_CONTAINER: u64 = 1 << 2;
pub const TAPIO_F_NODE_PMC: u64 = 1 << 3;
pub const TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES: usize = 16;

/// Shared agent -> eBPF runtime config ABI — 120 bytes.
///
/// Layout changes require bumping TAPIO_CONFIG_ABI_VERSION on both sides.
#[repr(C)]
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct TapioConfig {
    pub abi_version: u32,
    pub generation: u32,
    pub flags: u64,
    pub slow_io_threshold_ns: u64,
    pub conn_refused_window_ns: u64,
    pub conn_refused_min_count: u32,
    pub rtt_spike_multiplier: u32,
    pub rtt_min_baseline_samples: u32,
    pub ignore_exit_count: u32,
    pub ignore_exit_codes: [i32; TAPIO_CONFIG_MAX_IGNORE_EXIT_CODES],
    pub _pad: u32,
}

impl TapioConfig {
    pub fn has_valid_abi(&self) -> bool {
        self.abi_version == TAPIO_CONFIG_ABI_VERSION
    }

    pub fn observer_enabled(&self, flag: u64) -> bool {
        self.has_valid_abi() && (self.flags & flag) != 0
    }
}

/// Network event from network_monitor.c — 84 bytes, packed.
/// Each event type uses its own named fields — no overloading.
///
/// C layout:
/// ```c
/// struct network_event {
///     __u32 config_generation; // 0
///     __u32 pid;               // 4
///     __u32 src_ip;            // 8
///     __u32 dst_ip;            // 12
///     __u8  src_ipv6[16];      // 16
///     __u8  dst_ipv6[16];      // 32
///     __u16 src_port;          // 48
///     __u16 dst_port;          // 50
///     __u16 family;            // 52
///     __u8  protocol;          // 54
///     __u8  event_type;        // 55
///     __u16 old_state;         // 56  TCP state (state change events)
///     __u16 new_state;         // 58  TCP state (state change events)
///     __u16 rtt_baseline_ms;   // 60  baseline RTT in ms (RTT spike events)
///     __u16 rtt_current_ms;    // 62  current RTT in ms (RTT spike events)
///     __u16 total_retrans;     // 64  total retransmits (retransmit events)
///     __u16 snd_cwnd;          // 66  congestion window (retransmit events)
///     __u8  comm[16];          // 68
/// } __attribute__((packed));   // 84 bytes
/// ```
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct NetworkEvent {
    pub config_generation: u32,
    pub pid: u32,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_ipv6: [u8; 16],
    pub dst_ipv6: [u8; 16],
    pub src_port: u16,
    pub dst_port: u16,
    pub family: u16,
    pub protocol: u8,
    pub event_type: u8,
    pub old_state: u16,
    pub new_state: u16,
    pub rtt_baseline_ms: u16,
    pub rtt_current_ms: u16,
    pub total_retrans: u16,
    pub snd_cwnd: u16,
    pub comm: [u8; 16],
}

impl NetworkEvent {
    pub fn comm_str(&self) -> &str {
        let bytes = &self.comm;
        let len = bytes.iter().position(|&b| b == 0).unwrap_or(bytes.len());
        std::str::from_utf8(&bytes[..len]).unwrap_or("<invalid>")
    }

    pub fn is_ipv6(&self) -> bool {
        let family = self.family;
        family == AF_INET6
    }

    pub fn src_ipv4_str(&self) -> String {
        let ip = self.src_ip;
        format!(
            "{}.{}.{}.{}",
            ip & 0xFF,
            (ip >> 8) & 0xFF,
            (ip >> 16) & 0xFF,
            (ip >> 24) & 0xFF,
        )
    }

    pub fn dst_ipv4_str(&self) -> String {
        let ip = self.dst_ip;
        format!(
            "{}.{}.{}.{}",
            ip & 0xFF,
            (ip >> 8) & 0xFF,
            (ip >> 16) & 0xFF,
            (ip >> 24) & 0xFF,
        )
    }

    pub fn src_ipv6_str(&self) -> String {
        let b = self.src_ipv6;
        format_ipv6(&b)
    }

    pub fn dst_ipv6_str(&self) -> String {
        let b = self.dst_ipv6;
        format_ipv6(&b)
    }

    pub fn src_ip_str(&self) -> String {
        if self.is_ipv6() {
            self.src_ipv6_str()
        } else {
            self.src_ipv4_str()
        }
    }

    pub fn dst_ip_str(&self) -> String {
        if self.is_ipv6() {
            self.dst_ipv6_str()
        } else {
            self.dst_ipv4_str()
        }
    }
}

/// Container event from container_monitor.c — 56 bytes, packed.
///
/// C layout:
/// ```c
/// struct container_event {
///     __u32 config_generation; // 0
///     __u64 memory_limit;      // 4
///     __u64 memory_usage;      // 12
///     __u64 timestamp_ns;      // 20
///     __u64 cgroup_id;         // 28  — userspace derives K8s pod context from this ID
///     __u32 type;              // 36
///     __u32 pid;               // 40
///     __u32 tid;               // 44
///     __s32 exit_code;         // 48
///     __s32 signal;            // 52
/// } __attribute__((packed));
/// ```
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct ContainerEvent {
    pub config_generation: u32,
    pub memory_limit: u64,
    pub memory_usage: u64,
    pub timestamp_ns: u64,
    pub cgroup_id: u64,
    pub event_type: u32,
    pub pid: u32,
    pub tid: u32,
    pub exit_code: i32,
    pub signal: i32,
}

impl ContainerEvent {
    pub fn is_oom(&self) -> bool {
        let et = self.event_type;
        et == CONTAINER_EVENT_OOM
    }

    pub fn is_exit(&self) -> bool {
        let et = self.event_type;
        et == CONTAINER_EVENT_EXIT
    }
}

/// Storage event from storage_monitor.c — 80 bytes.
///
/// C layout:
/// ```c
/// struct storage_event {
///     __u32 config_generation; // 0
///     __u32 _pad0;             // 4
///     __u64 timestamp_ns;      // 8
///     __u64 latency_ns;        // 16
///     __u64 cgroup_id;         // 24
///     __u64 sector;            // 32
///     __u32 dev_major;         // 40
///     __u32 dev_minor;         // 44
///     __u32 bytes;             // 48
///     __u32 pid;               // 52
///     __s32 error_code;        // 56
///     __u8  opcode;            // 60
///     __u8  severity;          // 61
///     __u8  comm[16];          // 62
///     __u8  _pad[2];           // 78
/// };
/// ```
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct StorageEvent {
    pub config_generation: u32,
    pub _pad0: u32,
    pub timestamp_ns: u64,
    pub latency_ns: u64,
    pub cgroup_id: u64,
    pub sector: u64,
    pub dev_major: u32,
    pub dev_minor: u32,
    pub bytes: u32,
    pub pid: u32,
    pub error_code: i32,
    pub opcode: u8,
    pub severity: u8,
    pub comm: [u8; 16],
    pub _pad: [u8; 2],
}

impl StorageEvent {
    pub fn comm_str(&self) -> &str {
        let len = self
            .comm
            .iter()
            .position(|&b| b == 0)
            .unwrap_or(self.comm.len());
        std::str::from_utf8(&self.comm[..len]).unwrap_or("<invalid>")
    }

    pub fn latency_ms(&self) -> f64 {
        self.latency_ns as f64 / 1_000_000.0
    }

    pub fn has_error(&self) -> bool {
        self.error_code != 0
    }
}

/// PMC event from node_pmc_monitor.c — 40 bytes, packed.
///
/// C layout:
/// ```c
/// struct pmc_event {
///     __u32 config_generation; // 0
///     __u32 cpu;               // 4
///     __u64 cycles;            // 8
///     __u64 instructions;      // 16
///     __u64 stall_cycles;      // 24
///     __u64 timestamp;         // 32
/// } __attribute__((packed));   // 40 bytes
/// ```
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct PmcEvent {
    pub config_generation: u32,
    pub cpu: u32,
    pub cycles: u64,
    pub instructions: u64,
    pub stall_cycles: u64,
    pub timestamp: u64,
}

impl PmcEvent {
    pub fn ipc(&self) -> f64 {
        let cycles = self.cycles;
        if cycles == 0 {
            return 0.0;
        }
        self.instructions as f64 / cycles as f64
    }

    pub fn stall_pct(&self) -> f64 {
        let cycles = self.cycles;
        if cycles == 0 {
            return 0.0;
        }
        (self.stall_cycles as f64 / cycles as f64) * 100.0
    }
}

fn format_ipv6(b: &[u8; 16]) -> String {
    use std::net::Ipv6Addr;
    let addr = Ipv6Addr::from(*b);
    addr.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::mem::{offset_of, size_of};

    #[test]
    fn tapio_config_size() {
        assert_eq!(size_of::<TapioConfig>(), 120);
    }

    #[test]
    fn tapio_config_offsets() {
        assert_eq!(offset_of!(TapioConfig, abi_version), 0);
        assert_eq!(offset_of!(TapioConfig, generation), 4);
        assert_eq!(offset_of!(TapioConfig, flags), 8);
        assert_eq!(offset_of!(TapioConfig, slow_io_threshold_ns), 16);
        assert_eq!(offset_of!(TapioConfig, conn_refused_window_ns), 24);
        assert_eq!(offset_of!(TapioConfig, conn_refused_min_count), 32);
        assert_eq!(offset_of!(TapioConfig, rtt_spike_multiplier), 36);
        assert_eq!(offset_of!(TapioConfig, rtt_min_baseline_samples), 40);
        assert_eq!(offset_of!(TapioConfig, ignore_exit_count), 44);
        assert_eq!(offset_of!(TapioConfig, ignore_exit_codes), 48);
        assert_eq!(offset_of!(TapioConfig, _pad), 112);
    }

    #[test]
    fn zeroed_tapio_config_is_inert() {
        let cfg = TapioConfig::default();
        assert_eq!(cfg.abi_version, 0);
        assert_eq!(cfg.generation, 0);
        assert_eq!(cfg.flags, 0);
        assert_eq!(cfg.ignore_exit_count, 0);
        assert!(!cfg.has_valid_abi());
        assert!(!cfg.observer_enabled(TAPIO_F_NETWORK));
    }

    #[test]
    fn version_mismatch_disables_observers() {
        let cfg = TapioConfig {
            abi_version: TAPIO_CONFIG_ABI_VERSION + 1,
            flags: TAPIO_F_NETWORK,
            ..TapioConfig::default()
        };
        assert!(!cfg.has_valid_abi());
        assert!(!cfg.observer_enabled(TAPIO_F_NETWORK));
    }

    #[test]
    fn flag_gating_requires_valid_abi_and_flag() {
        let cfg = TapioConfig {
            abi_version: TAPIO_CONFIG_ABI_VERSION,
            flags: TAPIO_F_NETWORK | TAPIO_F_STORAGE | TAPIO_F_CONTAINER | TAPIO_F_NODE_PMC,
            ..TapioConfig::default()
        };
        assert!(cfg.observer_enabled(TAPIO_F_NETWORK));
        assert!(cfg.observer_enabled(TAPIO_F_STORAGE));
        assert!(cfg.observer_enabled(TAPIO_F_CONTAINER));
        assert!(cfg.observer_enabled(TAPIO_F_NODE_PMC));
        assert!(!cfg.observer_enabled(1 << 31));
    }

    #[test]
    fn network_event_size() {
        assert_eq!(size_of::<NetworkEvent>(), 84);
    }

    #[test]
    fn network_event_offsets() {
        assert_eq!(offset_of!(NetworkEvent, config_generation), 0);
        assert_eq!(offset_of!(NetworkEvent, pid), 4);
        assert_eq!(offset_of!(NetworkEvent, src_ip), 8);
        assert_eq!(offset_of!(NetworkEvent, dst_ip), 12);
        assert_eq!(offset_of!(NetworkEvent, src_ipv6), 16);
        assert_eq!(offset_of!(NetworkEvent, dst_ipv6), 32);
        assert_eq!(offset_of!(NetworkEvent, src_port), 48);
        assert_eq!(offset_of!(NetworkEvent, dst_port), 50);
        assert_eq!(offset_of!(NetworkEvent, family), 52);
        assert_eq!(offset_of!(NetworkEvent, protocol), 54);
        assert_eq!(offset_of!(NetworkEvent, event_type), 55);
        assert_eq!(offset_of!(NetworkEvent, old_state), 56);
        assert_eq!(offset_of!(NetworkEvent, new_state), 58);
        assert_eq!(offset_of!(NetworkEvent, rtt_baseline_ms), 60);
        assert_eq!(offset_of!(NetworkEvent, rtt_current_ms), 62);
        assert_eq!(offset_of!(NetworkEvent, total_retrans), 64);
        assert_eq!(offset_of!(NetworkEvent, snd_cwnd), 66);
        assert_eq!(offset_of!(NetworkEvent, comm), 68);
    }

    #[test]
    fn container_event_size() {
        assert_eq!(size_of::<ContainerEvent>(), 56);
    }

    #[test]
    fn container_event_offsets() {
        assert_eq!(offset_of!(ContainerEvent, config_generation), 0);
        assert_eq!(offset_of!(ContainerEvent, memory_limit), 4);
        assert_eq!(offset_of!(ContainerEvent, memory_usage), 12);
        assert_eq!(offset_of!(ContainerEvent, timestamp_ns), 20);
        assert_eq!(offset_of!(ContainerEvent, cgroup_id), 28);
        assert_eq!(offset_of!(ContainerEvent, event_type), 36);
        assert_eq!(offset_of!(ContainerEvent, pid), 40);
        assert_eq!(offset_of!(ContainerEvent, tid), 44);
        assert_eq!(offset_of!(ContainerEvent, exit_code), 48);
        assert_eq!(offset_of!(ContainerEvent, signal), 52);
    }

    #[test]
    fn storage_event_size() {
        assert_eq!(size_of::<StorageEvent>(), 80);
    }

    #[test]
    fn storage_event_offsets() {
        assert_eq!(offset_of!(StorageEvent, config_generation), 0);
        assert_eq!(offset_of!(StorageEvent, _pad0), 4);
        assert_eq!(offset_of!(StorageEvent, timestamp_ns), 8);
        assert_eq!(offset_of!(StorageEvent, latency_ns), 16);
        assert_eq!(offset_of!(StorageEvent, cgroup_id), 24);
        assert_eq!(offset_of!(StorageEvent, sector), 32);
        assert_eq!(offset_of!(StorageEvent, dev_major), 40);
        assert_eq!(offset_of!(StorageEvent, dev_minor), 44);
        assert_eq!(offset_of!(StorageEvent, bytes), 48);
        assert_eq!(offset_of!(StorageEvent, pid), 52);
        assert_eq!(offset_of!(StorageEvent, error_code), 56);
        assert_eq!(offset_of!(StorageEvent, opcode), 60);
        assert_eq!(offset_of!(StorageEvent, severity), 61);
        assert_eq!(offset_of!(StorageEvent, comm), 62);
        assert_eq!(offset_of!(StorageEvent, _pad), 78);
    }

    #[test]
    fn pmc_event_size() {
        assert_eq!(size_of::<PmcEvent>(), 40);
    }

    #[test]
    fn pmc_event_offsets() {
        assert_eq!(offset_of!(PmcEvent, config_generation), 0);
        assert_eq!(offset_of!(PmcEvent, cpu), 4);
        assert_eq!(offset_of!(PmcEvent, cycles), 8);
        assert_eq!(offset_of!(PmcEvent, instructions), 16);
        assert_eq!(offset_of!(PmcEvent, stall_cycles), 24);
        assert_eq!(offset_of!(PmcEvent, timestamp), 32);
    }

    #[test]
    fn pmc_event_ipc_and_stall() {
        let evt = unsafe {
            let mut e = std::mem::zeroed::<PmcEvent>();
            e.cpu = 0;
            e.cycles = 1000;
            e.instructions = 800;
            e.stall_cycles = 200;
            e.timestamp = 999;
            e
        };
        assert!((evt.ipc() - 0.8).abs() < 1e-12);
        assert!((evt.stall_pct() - 20.0).abs() < 1e-12);
    }

    #[test]
    fn pmc_event_zero_cycles() {
        let evt = unsafe { std::mem::zeroed::<PmcEvent>() };
        assert!((evt.ipc()).abs() < 1e-12);
        assert!((evt.stall_pct()).abs() < 1e-12);
    }

    #[test]
    fn network_event_comm_str() {
        let mut evt = unsafe { std::mem::zeroed::<NetworkEvent>() };
        evt.comm[0] = b'n';
        evt.comm[1] = b'g';
        evt.comm[2] = b'i';
        evt.comm[3] = b'n';
        evt.comm[4] = b'x';
        assert_eq!(evt.comm_str(), "nginx");
    }

    #[test]
    fn network_event_ipv4_str() {
        let mut evt = unsafe { std::mem::zeroed::<NetworkEvent>() };
        evt.src_ip = 0x0100007f; // 127.0.0.1 in network byte order (LE)
        assert_eq!(evt.src_ipv4_str(), "127.0.0.1");
    }

    #[test]
    fn network_event_ipv6() {
        let mut evt = unsafe { std::mem::zeroed::<NetworkEvent>() };
        evt.family = AF_INET6;
        evt.src_ipv6[15] = 1; // ::1
        assert!(evt.is_ipv6());
        assert_eq!(evt.src_ipv6_str(), "::1");
    }

    #[test]
    fn storage_event_latency() {
        let mut evt = unsafe { std::mem::zeroed::<StorageEvent>() };
        evt.latency_ns = 5_000_000; // 5ms
        assert!((evt.latency_ms() - 5.0).abs() < f64::EPSILON);
    }
}
