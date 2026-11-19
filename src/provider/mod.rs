use crate::domain::{CiTagInspection, Project, Runner, User};
use async_trait::async_trait;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ProviderError {
    #[error("Authentication failed: {0}")]
    Authentication(String),
    #[error("Permission denied: {0}")]
    Authorization(String),
    #[error("Resource not found: {0}")]
    NotFound(String),
    #[error("Conflict / resource modified: {0}")]
    Conflict(String),
    #[error("API request failed ({status}): {message}")]
    Api { status: u16, message: String },
    #[error("Network or connection error: {0}")]
    Network(String),
    #[error("Parse error: {0}")]
    Parse(String),
}

#[async_trait]
pub trait Provider: Send + Sync {
    fn name(&self) -> &'static str;

    async fn info(&self) -> Result<String, ProviderError>;

    async fn list_projects(
        &self,
        group: &str,
        recursive: bool,
    ) -> Result<Vec<Project>, ProviderError>;

    async fn delete_project(&self, project_id: &str) -> Result<(), ProviderError>;

    async fn list_group_members(&self, group: &str) -> Result<Vec<User>, ProviderError>;

    async fn remove_group_member(&self, group: &str, user_id: &str) -> Result<(), ProviderError>;

    async fn list_runners(&self, group: &str) -> Result<Vec<Runner>, ProviderError>;

    async fn update_runner_tags(
        &self,
        runner_id: &str,
        tags: Vec<String>,
    ) -> Result<(), ProviderError>;

    async fn inspect_pipeline_tags(
        &self,
        project_id: &str,
    ) -> Result<Option<CiTagInspection>, ProviderError>;

    async fn propose_pipeline_tag_change(
        &self,
        project_id: &str,
        old_tag: &str,
        new_tag: &str,
    ) -> Result<String, ProviderError>;
}
