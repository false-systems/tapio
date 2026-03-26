/// eBPF event structs matching C struct layouts.
/// These are the #[repr(C)] mirrors of the structs defined in ebpf/*.c

/// Network event from network_monitor.c (70 bytes packed)
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct NetworkEvent {
    pub timestamp: u64,
    pub src_ip: u32,
    pub dst_ip: u32,
    pub src_ipv6: [u8; 16],
    pub dst_ipv6: [u8; 16],
    pub src_port: u16,
    pub dst_port: u16,
    pub protocol: u8,
    pub family: u8,
    pub old_state: u8,
    pub new_state: u8,
    pub event_type: u8,
    pub comm: [u8; 16],
}

/// Container event from container_monitor.c (308 bytes)
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct ContainerEvent {
    pub timestamp: u64,
    pub cgroup_id: u64,
    pub memory_usage: u64,
    pub memory_limit: u64,
    pub pid: u32,
    pub tid: u32,
    pub exit_code: i32,
    pub signal: i32,
    pub event_type: u32,
    pub cgroup_path: [u8; 256],
}

/// Storage event from storage_monitor.c (72 bytes)
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct StorageEvent {
    pub start_ns: u64,
    pub duration_ns: u64,
    pub sector: u64,
    pub cgroup_id: u64,
    pub dev_major: u32,
    pub dev_minor: u32,
    pub bytes: u32,
    pub pid: u32,
    pub error: u16,
    pub operation: u8,
    pub severity: u8,
    pub comm: [u8; 16],
    pub _pad: [u8; 4],
}

/// PMC event from node_pmc_monitor.c (36 bytes packed)
/// Note: C struct is __attribute__((packed)), read manually not via cast
#[derive(Debug, Clone, Copy)]
pub struct PmcEvent {
    pub cpu: u32,
    pub cycles: u64,
    pub instructions: u64,
    pub stall_cycles: u64,
    pub timestamp: u64,
}

impl PmcEvent {
    /// Parse from raw bytes (packed C struct, 36 bytes)
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
}
