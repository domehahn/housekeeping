use chrono::Duration;
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_gitlab_url")]
    pub gitlab_url: String,
    #[serde(default = "default_token_env")]
    pub token_env: String,
    pub group: Option<String>,
    #[serde(default)]
    pub recursive: bool,
    #[serde(default)]
    pub policies: PoliciesConfig,
    #[serde(default)]
    pub safety: SafetyConfig,
}

fn default_gitlab_url() -> String {
    "https://gitlab.com".to_string()
}

fn default_token_env() -> String {
    "GITLAB_TOKEN".to_string()
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            gitlab_url: default_gitlab_url(),
            token_env: default_token_env(),
            group: None,
            recursive: false,
            policies: PoliciesConfig::default(),
            safety: SafetyConfig::default(),
        }
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PoliciesConfig {
    #[serde(default)]
    pub project: ProjectPolicyConfig,
    #[serde(default)]
    pub user: UserPolicyConfig,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProjectPolicyConfig {
    #[serde(default = "default_project_inactive_days")]
    pub inactive_days: u32,
    #[serde(default)]
    pub include_archived: bool,
    #[serde(default)]
    pub protect_names: Vec<String>,
    #[serde(default)]
    pub require_empty: bool,
}

fn default_project_inactive_days() -> u32 {
    180
}

impl Default for ProjectPolicyConfig {
    fn default() -> Self {
        Self {
            inactive_days: default_project_inactive_days(),
            include_archived: false,
            protect_names: vec![],
            require_empty: false,
        }
    }
}

impl ProjectPolicyConfig {
    pub fn inactive_duration(&self) -> Duration {
        Duration::days(self.inactive_days as i64)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserPolicyConfig {
    #[serde(default = "default_user_inactive_days")]
    pub inactive_days: u32,
    #[serde(default = "default_true")]
    pub protect_admins: bool,
    #[serde(default = "default_true")]
    pub protect_bots: bool,
    #[serde(default)]
    pub protect_usernames: Vec<String>,
    #[serde(default = "default_true")]
    pub billable_seat_override: bool,
}

fn default_user_inactive_days() -> u32 {
    90
}

fn default_true() -> bool {
    true
}

impl Default for UserPolicyConfig {
    fn default() -> Self {
        Self {
            inactive_days: default_user_inactive_days(),
            protect_admins: true,
            protect_bots: true,
            protect_usernames: vec![],
            billable_seat_override: true,
        }
    }
}

impl UserPolicyConfig {
    pub fn inactive_duration(&self) -> Duration {
        Duration::days(self.inactive_days as i64)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SafetyConfig {
    #[serde(default = "default_max_deletions")]
    pub max_deletions: usize,
    #[serde(default)]
    pub dry_run: bool,
}

fn default_max_deletions() -> usize {
    20
}

impl Default for SafetyConfig {
    fn default() -> Self {
        Self {
            max_deletions: default_max_deletions(),
            dry_run: true,
        }
    }
}

impl AppConfig {
    pub fn load_from_file<P: AsRef<Path>>(path: P) -> anyhow::Result<Self> {
        let content = std::fs::read_to_string(path)?;
        let config: AppConfig = serde_yaml::from_str(&content)?;
        Ok(config)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let cfg = AppConfig::default();
        assert_eq!(cfg.policies.project.inactive_days, 180);
        assert_eq!(cfg.policies.user.inactive_days, 90);
        assert_eq!(cfg.safety.max_deletions, 20);
    }
}
