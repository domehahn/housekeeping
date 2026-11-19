use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Branch {
    pub name: String,
    pub merged: bool,
    pub protected: bool,
    pub default: bool,
    pub commit_id: String,
    pub committed_date: Option<DateTime<Utc>>,
}
