use crate::store;
use std::collections::HashMap;
use std::path::Path;

pub fn run(dir: &Path, json: bool) -> anyhow::Result<()> {
    let occs = store::read_occurrences(dir, None)?;

    let mut by_severity: HashMap<String, usize> = HashMap::new();
    let mut by_type: HashMap<String, (usize, String)> = HashMap::new();

    for occ in &occs {
        let sev = format!("{:?}", occ.severity).to_lowercase();
        *by_severity.entry(sev).or_default() += 1;

        let entry = by_type
            .entry(occ.occurrence_type.clone())
            .or_insert((0, String::new()));
        entry.0 += 1;
        if entry.1 < occ.timestamp {
            entry.1.clone_from(&occ.timestamp);
        }
    }

    if json {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "total": occs.len(),
                "by_severity": by_severity,
                "by_type": by_type.iter().map(|(k, (count, latest))| {
                    serde_json::json!({"type": k, "count": count, "latest": latest})
                }).collect::<Vec<_>>(),
            }))?
        );
        return Ok(());
    }

    if occs.is_empty() {
        println!("no anomalies recorded");
        return Ok(());
    }

    println!("{:<12} COUNT", "SEVERITY");
    let mut sev_sorted: Vec<_> = by_severity.into_iter().collect();
    sev_sorted.sort_by(|a, b| b.1.cmp(&a.1));
    for (sev, count) in &sev_sorted {
        println!("{sev:<12} {count}");
    }

    println!();
    println!("{:<38} {:<6} LATEST", "TYPE", "COUNT");
    let mut type_sorted: Vec<_> = by_type.into_iter().collect();
    type_sorted.sort_by(|a, b| b.1.0.cmp(&a.1.0));
    for (typ, (count, latest)) in &type_sorted {
        let ts = latest.get(11..19).unwrap_or(latest);
        println!("{typ:<38} {count:<6} {ts}");
    }

    Ok(())
}
