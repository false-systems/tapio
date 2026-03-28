use crate::store;
use std::collections::HashMap;
use std::path::Path;

pub fn run(dir: &Path, json: bool) -> anyhow::Result<()> {
    let occs = store::read_occurrences(dir, None)?;

    if json {
        let mut types: HashMap<&str, usize> = HashMap::new();
        for occ in &occs {
            *types.entry(occ.occurrence_type.as_str()).or_default() += 1;
        }
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "count": occs.len(),
                "latest": occs.first().map(|o| &o.timestamp),
                "types": types,
            }))?
        );
        return Ok(());
    }

    println!("tapio v4 — kernel eyes");
    println!("  occurrences: {}", occs.len());

    if let Some(latest) = occs.first() {
        println!("  latest: {}", latest.timestamp);
        if let Some(ctx) = &latest.context {
            if let Some(node) = &ctx.node {
                print!("  node: {node}");
            }
            if let Some(cluster) = &ctx.cluster {
                print!(", cluster: {cluster}");
            }
            println!();
        }
    }

    if !occs.is_empty() {
        let mut types: HashMap<String, usize> = HashMap::new();
        for occ in &occs {
            let prefix = occ
                .occurrence_type
                .rsplitn(2, '.')
                .last()
                .unwrap_or(&occ.occurrence_type);
            *types.entry(format!("{prefix}.*")).or_default() += 1;
        }
        let mut sorted: Vec<_> = types.into_iter().collect();
        sorted.sort_by(|a, b| b.1.cmp(&a.1));
        let parts: Vec<String> = sorted.iter().map(|(k, v)| format!("{k} ({v})")).collect();
        println!("  types: {}", parts.join(", "));
    }

    Ok(())
}
