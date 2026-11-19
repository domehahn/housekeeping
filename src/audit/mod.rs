use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::fs::OpenOptions;
use std::io::Write;
use std::path::{Path, PathBuf};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditRecord {
    pub timestamp: DateTime<Utc>,
    pub action_id: String,
    pub action_kind: String,
    pub target_id: String,
    pub target_name: String,
    pub dry_run: bool,
    pub success: bool,
    pub error: Option<String>,
}

pub struct AuditLogger {
    path: PathBuf,
}

impl AuditLogger {
    pub fn new<P: AsRef<Path>>(path: P) -> Self {
        Self {
            path: path.as_ref().to_path_buf(),
        }
    }

    pub fn log(&self, record: &AuditRecord) -> std::io::Result<()> {
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)?;

        let line = serde_json::to_string(record)?;
        writeln!(file, "{}", line)?;
        Ok(())
    }
}
