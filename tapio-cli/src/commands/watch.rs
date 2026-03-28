use crate::commands::recent;
use crate::store;
use std::collections::HashSet;
use std::path::Path;
use std::time::Duration;

pub async fn run(dir: &Path, json: bool) -> anyhow::Result<()> {
    let mut seen: HashSet<String> = HashSet::new();

    // Load existing IDs so we only show new ones
    if let Ok(existing) = store::read_occurrences(dir, None) {
        for occ in &existing {
            seen.insert(occ.id.clone());
        }
    }

    if !json {
        println!("{:<24} {:<9} {:<38} POD", "TIMESTAMP", "SEVERITY", "TYPE");
    }

    loop {
        if let Ok(occs) = store::read_occurrences(dir, Some(50)) {
            for occ in occs.iter().rev() {
                if seen.insert(occ.id.clone()) {
                    if json {
                        if let Ok(j) = serde_json::to_string(&occ) {
                            println!("{j}");
                        }
                    } else {
                        recent::print_occurrence_line(occ);
                    }
                }
            }
        }
        tokio::time::sleep(Duration::from_millis(500)).await;
    }
}
