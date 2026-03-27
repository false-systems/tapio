use std::fs;
use std::path::PathBuf;
use std::sync::Once;
use tapio_common::occurrence::Occurrence;
use tapio_common::sink::{Sink, SinkError};

pub struct FileSink {
    dir: PathBuf,
    init: Once,
}

impl FileSink {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self {
            dir: dir.into(),
            init: Once::new(),
        }
    }

    fn ensure_dir(&self) -> Result<(), SinkError> {
        let dir = &self.dir;
        self.init.call_once(|| {
            let _ = fs::create_dir_all(dir);
        });
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
