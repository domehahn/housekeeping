use crate::config::ProjectPolicyConfig;
use crate::domain::clock::Clock;
use crate::domain::project::Project;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProjectDecision {
    Stale { reason: String },
    Keep { reason: String },
}

pub struct ProjectPolicy {
    config: ProjectPolicyConfig,
}

impl ProjectPolicy {
    pub fn new(config: ProjectPolicyConfig) -> Self {
        Self { config }
    }

    pub fn evaluate(&self, project: &Project, clock: &dyn Clock) -> ProjectDecision {
        // 1. Check protected names
        for pattern in &self.config.protect_names {
            if project.name.contains(pattern) || project.path_with_namespace.contains(pattern) {
                return ProjectDecision::Keep {
                    reason: format!("Matches protected pattern '{}'", pattern),
                };
            }
        }

        // 2. Check archived
        if project.archived && !self.config.include_archived {
            return ProjectDecision::Keep {
                reason: "Project is archived and archived projects are excluded".to_string(),
            };
        }

        // 3. Check require empty
        if self.config.require_empty && !project.empty_repo {
            return ProjectDecision::Keep {
                reason: "Project repository is not empty".to_string(),
            };
        }

        // 4. Activity check (Fail-closed: if activity is unknown, keep)
        let last_activity = match project.last_activity_at {
            Some(ts) => ts,
            None => {
                return ProjectDecision::Keep {
                    reason: "Last activity timestamp is unknown (fail-closed safe default)"
                        .to_string(),
                };
            }
        };

        let now = clock.now();
        let cutoff = now - self.config.inactive_duration();

        if last_activity < cutoff {
            let age_days = (now - last_activity).num_days();
            ProjectDecision::Stale {
                reason: format!(
                    "No activity for {} days (threshold: {} days)",
                    age_days, self.config.inactive_days
                ),
            }
        } else {
            ProjectDecision::Keep {
                reason: format!("Active within the last {} days", self.config.inactive_days),
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::clock::FixedClock;
    use chrono::{Duration, TimeZone, Utc};

    #[test]
    fn test_stale_project_evaluation() {
        let now = Utc.with_ymd_and_hms(2026, 11, 1, 12, 0, 0).unwrap();
        let clock = FixedClock::new(now);

        let mut config = ProjectPolicyConfig::default();
        config.inactive_days = 90;
        let policy = ProjectPolicy::new(config);

        let stale_proj = Project {
            id: "1".into(),
            name: "sandbox-old".into(),
            path_with_namespace: "my-group/sandbox-old".into(),
            description: None,
            web_url: "https://gitlab.example.com/my-group/sandbox-old".into(),
            archived: false,
            empty_repo: false,
            default_branch: Some("main".into()),
            created_at: now - Duration::days(200),
            last_activity_at: Some(now - Duration::days(120)),
            visibility: "private".into(),
        };

        let decision = policy.evaluate(&stale_proj, &clock);
        assert!(matches!(decision, ProjectDecision::Stale { .. }));

        let active_proj = Project {
            last_activity_at: Some(now - Duration::days(10)),
            ..stale_proj.clone()
        };
        let active_decision = policy.evaluate(&active_proj, &clock);
        assert!(matches!(active_decision, ProjectDecision::Keep { .. }));
    }
}
