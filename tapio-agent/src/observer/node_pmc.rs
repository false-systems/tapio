use tapio_common::ebpf::*;
use tapio_common::events::*;
use tapio_common::occurrence::{Occurrence, Outcome, Severity};

const STALL_PCT_WARNING: f64 = 20.0;
const STALL_PCT_CRITICAL: f64 = 40.0;
const IPC_DEGRADATION_THRESHOLD: f64 = 1.0;

pub struct ClassifiedAnomaly {
    pub event_type: &'static str,
    pub severity: Severity,
    pub outcome: Outcome,
    pub error_code: &'static str,
    pub error_message: String,
}

pub fn classify(event: &PmcEvent) -> Option<ClassifiedAnomaly> {
    let ipc = event.ipc();
    let stall_pct = event.stall_pct();

    if stall_pct >= STALL_PCT_CRITICAL {
        return Some(ClassifiedAnomaly {
            event_type: NODE_CPU_STALL,
            severity: Severity::Critical,
            outcome: Outcome::InProgress,
            error_code: "CPU_STALL",
            error_message: format!("CPU {} stall {stall_pct:.1}% (ipc={ipc:.2})", event.cpu,),
        });
    }

    if stall_pct >= STALL_PCT_WARNING {
        return Some(ClassifiedAnomaly {
            event_type: NODE_MEMORY_PRESSURE,
            severity: Severity::Warning,
            outcome: Outcome::InProgress,
            error_code: "MEMORY_PRESSURE",
            error_message: format!(
                "CPU {} memory pressure stall {stall_pct:.1}% (ipc={ipc:.2})",
                event.cpu,
            ),
        });
    }

    if ipc < IPC_DEGRADATION_THRESHOLD && event.cycles > 0 {
        return Some(ClassifiedAnomaly {
            event_type: NODE_IPC_DEGRADATION,
            severity: Severity::Warning,
            outcome: Outcome::InProgress,
            error_code: "IPC_DEGRADATION",
            error_message: format!("CPU {} low IPC {ipc:.2} (stall {stall_pct:.1}%)", event.cpu,),
        });
    }

    None
}

pub fn build_occurrence(event: &PmcEvent, anomaly: &ClassifiedAnomaly) -> Occurrence {
    Occurrence::new(
        anomaly.event_type,
        anomaly.severity.clone(),
        anomaly.outcome.clone(),
    )
    .with_error(anomaly.error_code, &anomaly.error_message)
    .with_data(serde_json::json!({
        "cpu": event.cpu,
        "cycles": event.cycles,
        "instructions": event.instructions,
        "stall_cycles": event.stall_cycles,
        "ipc": event.ipc(),
        "stall_pct": event.stall_pct(),
        "timestamp_ns": event.timestamp,
    }))
}

// PMC observer requires perf_event_open syscalls to set up hardware counters.
// The classify + build_occurrence functions work and are tested.
// The run() wiring is deferred until perf event FD setup is implemented.

#[cfg(test)]
mod tests {
    use super::*;

    fn make_pmc(cycles: u64, instructions: u64, stall_cycles: u64) -> PmcEvent {
        PmcEvent {
            cpu: 0,
            cycles,
            instructions,
            stall_cycles,
            timestamp: 1_000_000_000,
        }
    }

    #[test]
    fn classify_critical_stall() {
        let evt = make_pmc(1000, 400, 500); // 50% stalls
        let a = classify(&evt).expect("should classify critical stall");
        assert_eq!(a.event_type, NODE_CPU_STALL);
        assert!(matches!(a.severity, Severity::Critical));
    }

    #[test]
    fn classify_memory_pressure() {
        let evt = make_pmc(1000, 600, 250); // 25% stalls
        let a = classify(&evt).expect("should classify memory pressure");
        assert_eq!(a.event_type, NODE_MEMORY_PRESSURE);
        assert!(matches!(a.severity, Severity::Warning));
    }

    #[test]
    fn classify_ipc_degradation() {
        let evt = make_pmc(1000, 500, 100); // IPC=0.5, stalls=10%
        let a = classify(&evt).expect("should classify IPC degradation");
        assert_eq!(a.event_type, NODE_IPC_DEGRADATION);
        assert!(a.error_message.contains("0.50"));
    }

    #[test]
    fn classify_normal_returns_none() {
        let evt = make_pmc(1000, 1500, 100); // IPC=1.5, stalls=10%
        assert!(classify(&evt).is_none());
    }

    #[test]
    fn classify_zero_cycles_returns_none() {
        let evt = make_pmc(0, 0, 0);
        assert!(classify(&evt).is_none());
    }

    #[test]
    fn build_occurrence_valid() {
        let evt = make_pmc(1000, 400, 500);
        let a = classify(&evt).unwrap();
        let occ = build_occurrence(&evt, &a);
        assert!(occ.validate().is_ok());
        assert_eq!(occ.occurrence_type, NODE_CPU_STALL);
        let data = occ.data.unwrap();
        assert_eq!(data["cpu"], 0);
    }
}
