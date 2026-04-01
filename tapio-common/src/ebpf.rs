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

/// Network event from network_monitor.c — 72 bytes, packed.
///
/// C layout:
/// ```c
/// struct network_event {
///     __u32 pid;            // 0
///     __u32 src_ip;         // 4
///     __u32 dst_ip;         // 8
///     __u8  src_ipv6[16];   // 12
///     __u8  dst_ipv6[16];   // 28
///     __u16 src_port;       // 44
///     __u16 dst_port;       // 46
///     __u16 family;         // 48
///     __u8  protocol;       // 50
///     __u8  event_type;     // 51
///     __u16 old_state;      // 52
///     __u16 new_state;      // 54
///     __u8  comm[16];       // 56
/// } __attribute__((packed));
/// ```
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct NetworkEvent {
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

/// Container event from container_monitor.c — 52 bytes, packed.
///
/// C layout:
/// ```c
/// struct container_event {
///     __u64 memory_limit;   // 0
///     __u64 memory_usage;   // 8
///     __u64 timestamp_ns;   // 16
///     __u64 cgroup_id;      // 24  — userspace resolves cgroup path via this ID
///     __u32 type;           // 32
///     __u32 pid;            // 36
///     __u32 tid;            // 40
///     __s32 exit_code;      // 44
///     __s32 signal;         // 48
/// } __attribute__((packed));
/// ```
#[repr(C, packed)]
#[derive(Debug, Clone, Copy)]
pub struct ContainerEvent {
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

/// Storage event from storage_monitor.c — 72 bytes.
///
/// C layout:
/// ```c
/// struct storage_event {
///     __u64 timestamp_ns;   // 0
///     __u64 latency_ns;     // 8
///     __u64 cgroup_id;      // 16
///     __u64 sector;         // 24
///     __u32 dev_major;      // 32
///     __u32 dev_minor;      // 36
///     __u32 bytes;          // 40
///     __u32 pid;            // 44
///     __u16 error_code;     // 48
///     __u8  opcode;         // 50
///     __u8  severity;       // 51
///     __u8  comm[16];       // 52
///     __u8  _pad[4];        // 68
/// };
/// ```
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct StorageEvent {
    pub timestamp_ns: u64,
    pub latency_ns: u64,
    pub cgroup_id: u64,
    pub sector: u64,
    pub dev_major: u32,
    pub dev_minor: u32,
    pub bytes: u32,
    pub pid: u32,
    pub error_code: u16,
    pub opcode: u8,
    pub severity: u8,
    pub comm: [u8; 16],
    pub _pad: [u8; 4],
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

/// PMC event from node_pmc_monitor.c — 36 bytes, packed.
/// Parsed manually because C struct is __attribute__((packed))
/// with u32 followed by u64 (no natural alignment).
#[derive(Debug, Clone, Copy)]
pub struct PmcEvent {
    pub cpu: u32,
    pub cycles: u64,
    pub instructions: u64,
    pub stall_cycles: u64,
    pub timestamp: u64,
}

impl PmcEvent {
    pub fn from_bytes(data: &[u8]) -> Option<Self> {
        if data.len() < 36 {
            return None;
        }
        Some(Self {
            cpu: u32::from_le_bytes(data[0..4].try_into().ok()?),
            cycles: u64::from_le_bytes(data[4..12].try_into().ok()?),
            instructions: u64::from_le_bytes(data[12..20].try_into().ok()?),
            stall_cycles: u64::from_le_bytes(data[20..28].try_into().ok()?),
            timestamp: u64::from_le_bytes(data[28..36].try_into().ok()?),
        })
    }

    pub fn ipc(&self) -> f64 {
        if self.cycles == 0 {
            return 0.0;
        }
        self.instructions as f64 / self.cycles as f64
    }

    pub fn stall_pct(&self) -> f64 {
        if self.cycles == 0 {
            return 0.0;
        }
        (self.stall_cycles as f64 / self.cycles as f64) * 100.0
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
    use std::mem::size_of;

    #[test]
    fn network_event_size() {
        assert_eq!(size_of::<NetworkEvent>(), 72);
    }

    #[test]
    fn container_event_size() {
        assert_eq!(size_of::<ContainerEvent>(), 52);
    }

    #[test]
    fn storage_event_size() {
        assert_eq!(size_of::<StorageEvent>(), 72);
    }

    #[test]
    fn pmc_event_from_bytes() {
        let mut data = [0u8; 36];
        data[0..4].copy_from_slice(&42u32.to_le_bytes()); // cpu
        data[4..12].copy_from_slice(&1000u64.to_le_bytes()); // cycles
        data[12..20].copy_from_slice(&800u64.to_le_bytes()); // instructions
        data[20..28].copy_from_slice(&200u64.to_le_bytes()); // stall_cycles
        data[28..36].copy_from_slice(&999u64.to_le_bytes()); // timestamp

        let evt = PmcEvent::from_bytes(&data).unwrap();
        assert_eq!(evt.cpu, 42);
        assert_eq!(evt.cycles, 1000);
        assert_eq!(evt.instructions, 800);
        assert_eq!(evt.stall_cycles, 200);
        assert_eq!(evt.timestamp, 999);
        assert!((evt.ipc() - 0.8).abs() < f64::EPSILON);
        assert!((evt.stall_pct() - 20.0).abs() < f64::EPSILON);
    }

    #[test]
    fn pmc_event_from_short_bytes() {
        assert!(PmcEvent::from_bytes(&[0u8; 35]).is_none());
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
