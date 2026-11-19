use super::activity::ActivityStatus;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum AccessLevel {
    Guest,
    Reporter,
    Developer,
    Maintainer,
    Owner,
    Custom(i32),
}

impl AccessLevel {
    pub fn from_gitlab(level: i32) -> Self {
        match level {
            10 => AccessLevel::Guest,
            20 => AccessLevel::Reporter,
            30 => AccessLevel::Developer,
            40 => AccessLevel::Maintainer,
            50 => AccessLevel::Owner,
            other => AccessLevel::Custom(other),
        }
    }

    pub fn to_display(&self) -> &'static str {
        match self {
            AccessLevel::Guest => "Guest",
            AccessLevel::Reporter => "Reporter",
            AccessLevel::Developer => "Developer",
            AccessLevel::Maintainer => "Maintainer",
            AccessLevel::Owner => "Owner",
            AccessLevel::Custom(_) => "Custom",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct User {
    pub id: String,
    pub username: String,
    pub name: String,
    pub email: Option<String>,
    pub state: String,
    pub is_bot: bool,
    pub is_admin: bool,
    pub access_level: AccessLevel,
    pub created_at: DateTime<Utc>,
    pub last_activity_at: ActivityStatus,
    pub last_login_at: ActivityStatus,
    pub is_billable: bool,
}
