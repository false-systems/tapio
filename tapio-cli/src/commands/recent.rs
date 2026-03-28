use crate::store;
use std::path::Path;
use tapio_common::occurrence::Occurrence;

pub fn run(dir: &Path, limit: usize, json: bool) -> anyhow::Result<()> {
    let occs = store::read_occurrences(dir, Some(limit))?;

    if json {
        println!("{}", serde_json::to_string_pretty(&occs)?);
        return Ok(());
    }

    if occs.is_empty() {
        println!("no anomalies yet");
        return Ok(());
    }

    println!("{:<24} {:<9} {:<38} POD", "TIMESTAMP", "SEVERITY", "TYPE");
    for occ in &occs {
        print_occurrence_line(occ);
    }

    Ok(())
}

pub fn print_occurrence_line(occ: &Occurrence) {
    let ts = occ
        .timestamp
        .get(..19)
        .unwrap_or(&occ.timestamp)
        .replace('T', " ");
    let severity = format!("{:?}", occ.severity).to_lowercase();
    let pod = store::pod_id(occ).unwrap_or("—");

    println!("{ts:<24} {severity:<9} {:<38} {pod}", occ.occurrence_type);
}
