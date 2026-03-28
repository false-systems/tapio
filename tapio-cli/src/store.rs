use std::fs;
use std::path::Path;
use tapio_common::occurrence::Occurrence;

pub fn read_occurrences(dir: &Path, limit: Option<usize>) -> anyhow::Result<Vec<Occurrence>> {
    if !dir.exists() {
        return Ok(vec![]);
    }

    let mut files: Vec<_> = fs::read_dir(dir)?
        .filter_map(|e| e.ok())
        .filter(|e| e.path().extension().is_some_and(|ext| ext == "json"))
        .collect();

    // ULID filenames are time-sortable — sort descending (newest first)
    files.sort_by_key(|f| std::cmp::Reverse(f.file_name()));

    if let Some(limit) = limit {
        files.truncate(limit);
    }

    let mut occurrences = Vec::with_capacity(files.len());
    for entry in files {
        match fs::read_to_string(entry.path()) {
            Ok(content) => match serde_json::from_str::<Occurrence>(&content) {
                Ok(occ) => occurrences.push(occ),
                Err(e) => {
                    tracing::warn!(file = ?entry.path(), error = %e, "skipping malformed occurrence");
                }
            },
            Err(e) => {
                tracing::warn!(file = ?entry.path(), error = %e, "skipping unreadable file");
            }
        }
    }

    Ok(occurrences)
}

pub fn read_occurrence(dir: &Path, id: &str) -> anyhow::Result<Occurrence> {
    let path = dir.join(format!("{id}.json"));
    let content =
        fs::read_to_string(&path).map_err(|_| anyhow::anyhow!("occurrence not found: {id}"))?;
    Ok(serde_json::from_str(&content)?)
}

/// Extract pod ID (ns/name) from occurrence context.
pub fn pod_id(occ: &Occurrence) -> Option<&str> {
    occ.context
        .as_ref()?
        .entities
        .iter()
        .find(|e| e.kind == "pod")
        .map(|e| e.id.as_str())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use tempfile::TempDir;

    fn write_occ(dir: &Path, id: &str, occ_type: &str, severity: &str) {
        let json = format!(
            r#"{{"id":"{id}","timestamp":"2026-03-27T14:23:01Z","source":"tapio","type":"{occ_type}","severity":"{severity}","outcome":"failure","protocol_version":"1.0"}}"#
        );
        fs::write(dir.join(format!("{id}.json")), json).unwrap();
    }

    #[test]
    fn read_occurrences_empty_dir() {
        let dir = TempDir::new().unwrap();
        let result = read_occurrences(dir.path(), None).unwrap();
        assert!(result.is_empty());
    }

    #[test]
    fn read_occurrences_nonexistent_dir() {
        let result = read_occurrences(Path::new("/nonexistent"), None).unwrap();
        assert!(result.is_empty());
    }

    #[test]
    fn read_occurrences_sorted_newest_first() {
        let dir = TempDir::new().unwrap();
        write_occ(dir.path(), "01AAA", "kernel.a", "info");
        write_occ(dir.path(), "01BBB", "kernel.b", "warning");
        write_occ(dir.path(), "01CCC", "kernel.c", "critical");

        let result = read_occurrences(dir.path(), None).unwrap();
        assert_eq!(result.len(), 3);
        assert_eq!(result[0].id, "01CCC"); // newest first
        assert_eq!(result[2].id, "01AAA");
    }

    #[test]
    fn read_occurrences_respects_limit() {
        let dir = TempDir::new().unwrap();
        write_occ(dir.path(), "01AAA", "kernel.a", "info");
        write_occ(dir.path(), "01BBB", "kernel.b", "warning");
        write_occ(dir.path(), "01CCC", "kernel.c", "critical");

        let result = read_occurrences(dir.path(), Some(2)).unwrap();
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn read_occurrences_skips_malformed() {
        let dir = TempDir::new().unwrap();
        write_occ(dir.path(), "01AAA", "kernel.a", "info");
        fs::write(dir.path().join("01BAD.json"), "not json").unwrap();

        let result = read_occurrences(dir.path(), None).unwrap();
        assert_eq!(result.len(), 1);
    }

    #[test]
    fn read_single_occurrence() {
        let dir = TempDir::new().unwrap();
        write_occ(dir.path(), "01ABC", "kernel.test", "warning");

        let occ = read_occurrence(dir.path(), "01ABC").unwrap();
        assert_eq!(occ.id, "01ABC");
    }
}
