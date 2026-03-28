use crate::commands::recent;
use crate::store;
use std::path::Path;

pub fn run(dir: &Path, pod: &str, json: bool) -> anyhow::Result<()> {
    let occs = store::read_occurrences(dir, None)?;

    let filtered: Vec<_> = occs
        .into_iter()
        .filter(|occ| {
            occ.context
                .as_ref()
                .map(|ctx| {
                    ctx.entities.iter().any(|e| {
                        e.kind == "pod" && (e.id.contains(pod) || e.name.as_deref() == Some(pod))
                    })
                })
                .unwrap_or(false)
        })
        .collect();

    if json {
        println!("{}", serde_json::to_string_pretty(&filtered)?);
        return Ok(());
    }

    if filtered.is_empty() {
        println!("no anomalies found for pod: {pod}");
        return Ok(());
    }

    println!("Anomalies for pod matching '{pod}':");
    println!();
    println!("{:<24} {:<9} {:<38} POD", "TIMESTAMP", "SEVERITY", "TYPE");
    for occ in &filtered {
        recent::print_occurrence_line(occ);
    }

    Ok(())
}
