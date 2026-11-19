use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "status", content = "timestamp")]
pub enum ActivityStatus {
    Known(DateTime<Utc>),
    Unknown,
    Never,
}

impl ActivityStatus {
    pub fn is_known(&self) -> bool {
        matches!(self, ActivityStatus::Known(_))
    }

    pub fn timestamp(&self) -> Option<DateTime<Utc>> {
        match self {
            ActivityStatus::Known(ts) => Some(*ts),
            _ => None,
        }
    }
}
