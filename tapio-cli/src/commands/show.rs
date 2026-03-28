use crate::store;
use std::path::Path;

pub fn run(dir: &Path, id: &str, json: bool) -> anyhow::Result<()> {
    let occ = store::read_occurrence(dir, id)?;

    if json {
        println!("{}", serde_json::to_string_pretty(&occ)?);
        return Ok(());
    }

    println!("{}", serde_json::to_string_pretty(&occ)?);
    Ok(())
}
