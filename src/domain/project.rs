use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Project {
    pub id: String,
    pub name: String,
    pub path_with_namespace: String,
    pub description: Option<String>,
    pub web_url: String,
    pub archived: bool,
    pub empty_repo: bool,
    pub default_branch: Option<String>,
    pub created_at: DateTime<Utc>,
    pub last_activity_at: Option<DateTime<Utc>>,
    pub visibility: String,
}
