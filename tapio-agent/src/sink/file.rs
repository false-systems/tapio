use std::fs;
use std::path::PathBuf;
use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

pub struct FileSink {
    dir: PathBuf,
}

impl FileSink {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self { dir: dir.into() }
    }

    fn ensure_dir(&self) -> Result<(), SinkError> {
        if !self.dir.exists() {
            fs::create_dir_all(&self.dir).map_err(SinkError::Io)?;
        }
        Ok(())
    }
}

impl Sink for FileSink {
    fn send(&self, occurrence: &Occurrence) -> Result<(), SinkError> {
        self.ensure_dir()?;
        let path = self.dir.join(format!("{}.json", occurrence.id));
        let data = serde_json::to_vec_pretty(occurrence)
            .map_err(|e| SinkError::Serialization(e.to_string()))?;
        fs::write(&path, data).map_err(SinkError::Io)?;
        Ok(())
    }

    fn flush(&self) -> Result<(), SinkError> {
        Ok(())
    }

    fn name(&self) -> &str {
        "file"
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tapio_common::occurrence::{Occurrence, Outcome, Severity};

    #[test]
    fn writes_event_json_that_round_trips() {
        let occ = Occurrence::new(
            "kernel.container.oom_kill",
            Severity::Critical,
            Outcome::Failure,
        )
        .with_error("OOM_KILL", "OOM kill pid=1234");

        let dir = std::env::temp_dir().join(format!("tapio-file-sink-{}", occ.id));
        let sink = FileSink::new(&dir);
        sink.send(&occ).unwrap();

        let path = dir.join(format!("{}.json", occ.id));
        let data = std::fs::read_to_string(&path).unwrap();
        let parsed: Occurrence = serde_json::from_str(&data).unwrap();

        assert_eq!(parsed.id, occ.id);
        assert_eq!(parsed.occurrence_type, "kernel.container.oom_kill");
        assert_eq!(parsed.source, "tapio");

        std::fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn name_is_stable() {
        assert_eq!(FileSink::new(".tapio").name(), "file");
    }
}
