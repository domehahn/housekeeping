use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PipelineJobTag {
    pub job_name: String,
    pub stage: Option<String>,
    pub tags: Vec<String>,
    pub file_path: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CiTagInspection {
    pub project_id: String,
    pub project_path: String,
    pub default_branch: String,
    pub ci_config_path: String,
    pub jobs: Vec<PipelineJobTag>,
    pub uses_tag: bool,
}
