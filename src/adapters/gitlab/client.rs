use crate::ciyaml::CiYamlPatcher;
use crate::domain::activity::ActivityStatus;
use crate::domain::user::AccessLevel;
use crate::domain::{CiTagInspection, PipelineJobTag, Project, Runner, User};
use crate::provider::{Provider, ProviderError};
use async_trait::async_trait;
use chrono::{DateTime, Utc};
use reqwest::header::{HeaderMap, HeaderValue};
use reqwest::{Client, StatusCode};
use serde::Deserialize;

pub struct GitLabAdapter {
    base_url: String,
    client: Client,
}

#[derive(Debug, Deserialize)]
struct GitLabVersion {
    version: String,
    revision: Option<String>,
}

#[derive(Debug, Deserialize)]
struct GitLabProject {
    id: u64,
    name: String,
    path_with_namespace: String,
    description: Option<String>,
    web_url: String,
    archived: Option<bool>,
    empty_repo: Option<bool>,
    default_branch: Option<String>,
    created_at: DateTime<Utc>,
    last_activity_at: Option<DateTime<Utc>>,
    visibility: Option<String>,
}

#[derive(Debug, Deserialize)]
struct GitLabMember {
    id: u64,
    username: String,
    name: String,
    state: Option<String>,
    access_level: i32,
    created_at: Option<DateTime<Utc>>,
    is_bot: Option<bool>,
    is_admin: Option<bool>,
}

#[derive(Debug, Deserialize)]
struct GitLabRunner {
    id: u64,
    description: Option<String>,
    ip_address: Option<String>,
    active: Option<bool>,
    paused: Option<bool>,
    is_shared: Option<bool>,
    runner_type: Option<String>,
    tag_list: Option<Vec<String>>,
    contacted_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Deserialize)]
#[allow(dead_code)]
struct GitLabMergeRequest {
    iid: u64,
    web_url: String,
}

impl GitLabAdapter {
    pub fn new(base_url: &str, token: &str) -> Result<Self, ProviderError> {
        let mut headers = HeaderMap::new();
        let mut token_val = HeaderValue::from_str(token).map_err(|e| {
            ProviderError::Authentication(format!("Invalid token characters: {}", e))
        })?;
        token_val.set_sensitive(true);
        headers.insert("PRIVATE-TOKEN", token_val);

        let client = Client::builder()
            .default_headers(headers)
            .build()
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        let clean_base = base_url.trim_end_matches('/').to_string();
        Ok(Self {
            base_url: clean_base,
            client,
        })
    }

    fn url(&self, path: &str) -> String {
        format!("{}/api/v4/{}", self.base_url, path.trim_start_matches('/'))
    }

    fn map_error(&self, status: StatusCode, body: &str) -> ProviderError {
        match status {
            StatusCode::UNAUTHORIZED => ProviderError::Authentication(
                "GitLab token is invalid, revoked, or expired".to_string(),
            ),
            StatusCode::FORBIDDEN => ProviderError::Authorization(
                "Token does not have sufficient permissions".to_string(),
            ),
            StatusCode::NOT_FOUND => {
                ProviderError::NotFound("Requested GitLab resource not found".to_string())
            }
            StatusCode::CONFLICT => ProviderError::Conflict(body.to_string()),
            _ => ProviderError::Api {
                status: status.as_u16(),
                message: body.to_string(),
            },
        }
    }
}

#[async_trait]
impl Provider for GitLabAdapter {
    fn name(&self) -> &'static str {
        "gitlab"
    }

    async fn info(&self) -> Result<String, ProviderError> {
        let resp = self
            .client
            .get(self.url("version"))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let ver: GitLabVersion = resp
            .json()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;

        Ok(format!(
            "GitLab instance at {} (Version: {}, Revision: {})",
            self.base_url,
            ver.version,
            ver.revision.unwrap_or_else(|| "none".into())
        ))
    }

    async fn list_projects(
        &self,
        group: &str,
        recursive: bool,
    ) -> Result<Vec<Project>, ProviderError> {
        let encoded_group = urlencoding_encode(group);
        let path = format!(
            "groups/{}/projects?include_subgroups={}&per_page=100",
            encoded_group, recursive
        );

        let resp = self
            .client
            .get(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let gl_projects: Vec<GitLabProject> = resp
            .json()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;

        let projects = gl_projects
            .into_iter()
            .map(|p| Project {
                id: p.id.to_string(),
                name: p.name,
                path_with_namespace: p.path_with_namespace,
                description: p.description,
                web_url: p.web_url,
                archived: p.archived.unwrap_or(false),
                empty_repo: p.empty_repo.unwrap_or(false),
                default_branch: p.default_branch,
                created_at: p.created_at,
                last_activity_at: p.last_activity_at,
                visibility: p.visibility.unwrap_or_else(|| "private".into()),
            })
            .collect();

        Ok(projects)
    }

    async fn delete_project(&self, project_id: &str) -> Result<(), ProviderError> {
        let path = format!("projects/{}", project_id);
        let resp = self
            .client
            .delete(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        Ok(())
    }

    async fn list_group_members(&self, group: &str) -> Result<Vec<User>, ProviderError> {
        let encoded_group = urlencoding_encode(group);
        let path = format!("groups/{}/members?per_page=100", encoded_group);

        let resp = self
            .client
            .get(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let gl_members: Vec<GitLabMember> = resp
            .json()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;

        let members = gl_members
            .into_iter()
            .map(|m| User {
                id: m.id.to_string(),
                username: m.username,
                name: m.name,
                email: None,
                state: m.state.unwrap_or_else(|| "active".into()),
                is_bot: m.is_bot.unwrap_or(false),
                is_admin: m.is_admin.unwrap_or(false),
                access_level: AccessLevel::from_gitlab(m.access_level),
                created_at: m.created_at.unwrap_or_else(Utc::now),
                last_activity_at: ActivityStatus::Unknown,
                last_login_at: ActivityStatus::Unknown,
                is_billable: m.access_level > 10,
            })
            .collect();

        Ok(members)
    }

    async fn remove_group_member(&self, group: &str, user_id: &str) -> Result<(), ProviderError> {
        let encoded_group = urlencoding_encode(group);
        let path = format!("groups/{}/members/{}", encoded_group, user_id);

        let resp = self
            .client
            .delete(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        Ok(())
    }

    async fn list_runners(&self, group: &str) -> Result<Vec<Runner>, ProviderError> {
        let encoded_group = urlencoding_encode(group);
        let path = format!("groups/{}/runners?per_page=100", encoded_group);

        let resp = self
            .client
            .get(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let gl_runners: Vec<GitLabRunner> = resp
            .json()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;

        let runners = gl_runners
            .into_iter()
            .map(|r| Runner {
                id: r.id.to_string(),
                description: r.description,
                ip_address: r.ip_address,
                active: r.active.unwrap_or(true),
                paused: r.paused.unwrap_or(false),
                is_shared: r.is_shared.unwrap_or(false),
                runner_type: r.runner_type.unwrap_or_else(|| "group_type".into()),
                tag_list: r.tag_list.unwrap_or_default(),
                contact_at: r.contacted_at,
            })
            .collect();

        Ok(runners)
    }

    async fn update_runner_tags(
        &self,
        runner_id: &str,
        tags: Vec<String>,
    ) -> Result<(), ProviderError> {
        let path = format!("runners/{}", runner_id);
        let body = serde_json::json!({
            "tag_list": tags
        });

        let resp = self
            .client
            .put(self.url(&path))
            .json(&body)
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        Ok(())
    }

    async fn inspect_pipeline_tags(
        &self,
        project_id: &str,
    ) -> Result<Option<CiTagInspection>, ProviderError> {
        let path = format!(
            "projects/{}/repository/files/.gitlab-ci.yml/raw",
            project_id
        );
        let resp = self
            .client
            .get(self.url(&path))
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if resp.status() == StatusCode::NOT_FOUND {
            return Ok(None);
        }

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let raw_yaml = resp
            .text()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;
        let jobs = CiYamlPatcher::find_tags(&raw_yaml);

        Ok(Some(CiTagInspection {
            project_id: project_id.to_string(),
            project_path: project_id.to_string(),
            default_branch: "main".into(),
            ci_config_path: ".gitlab-ci.yml".into(),
            uses_tag: !jobs.is_empty(),
            jobs: jobs
                .into_iter()
                .map(|(job, tags)| PipelineJobTag {
                    job_name: job,
                    stage: None,
                    tags,
                    file_path: ".gitlab-ci.yml".into(),
                })
                .collect(),
        }))
    }

    async fn propose_pipeline_tag_change(
        &self,
        project_id: &str,
        old_tag: &str,
        new_tag: &str,
    ) -> Result<String, ProviderError> {
        let mr_path = format!("projects/{}/merge_requests", project_id);
        let branch_name = format!("scm-cleaner/replace-tag-{}-{}", old_tag, new_tag);
        let body = serde_json::json!({
            "source_branch": branch_name,
            "target_branch": "main",
            "title": format!("ci: replace runner tag '{}' with '{}'", old_tag, new_tag),
            "description": format!("Automated proposal by scm-cleaner to update runner tag from `{}` to `{}`.", old_tag, new_tag)
        });

        let resp = self
            .client
            .post(self.url(&mr_path))
            .json(&body)
            .send()
            .await
            .map_err(|e| ProviderError::Network(e.to_string()))?;

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return Err(self.map_error(status, &body));
        }

        let mr: GitLabMergeRequest = resp
            .json()
            .await
            .map_err(|e| ProviderError::Parse(e.to_string()))?;

        Ok(mr.web_url)
    }
}

fn urlencoding_encode(s: &str) -> String {
    s.replace('/', "%2F")
}
