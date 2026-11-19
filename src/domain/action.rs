use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum ActionKind {
    DeleteProject,
    ArchiveProject,
    RemoveGroupMember,
    UpdateRunnerTags,
    ProposePipelineTagChange,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Action {
    pub id: String,
    pub kind: ActionKind,
    pub target_id: String,
    pub target_name: String,
    pub description: String,
    pub reasons: Vec<String>,
    pub metadata: std::collections::HashMap<String, String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Plan {
    pub version: String,
    pub generated_at: DateTime<Utc>,
    pub group: String,
    pub provider: String,
    pub actions: Vec<Action>,
    pub dry_run: bool,
}

impl Plan {
    pub fn new(
        group: impl Into<String>,
        provider: impl Into<String>,
        actions: Vec<Action>,
    ) -> Self {
        Self {
            version: "v1".to_string(),
            generated_at: Utc::now(),
            group: group.into(),
            provider: provider.into(),
            actions,
            dry_run: true,
        }
    }
}
