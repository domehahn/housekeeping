use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Runner {
    pub id: String,
    pub description: Option<String>,
    pub ip_address: Option<String>,
    pub active: bool,
    pub paused: bool,
    pub is_shared: bool,
    pub runner_type: String,
    pub tag_list: Vec<String>,
    pub contact_at: Option<DateTime<Utc>>,
}
