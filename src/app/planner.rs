use crate::config::AppConfig;
use crate::domain::action::{Action, ActionKind, Plan};
use crate::domain::clock::Clock;
use crate::domain::{Project, Runner, User};
use crate::policy::project::{ProjectDecision, ProjectPolicy};
use crate::policy::runner::{RunnerDecision, RunnerTagPolicy};
use crate::policy::user::{UserDecision, UserPolicy};
use std::collections::HashMap;

pub struct Planner;

impl Planner {
    pub fn plan_projects(projects: &[Project], config: &AppConfig, clock: &dyn Clock) -> Plan {
        let policy = ProjectPolicy::new(config.policies.project.clone());
        let mut actions = Vec::new();

        for proj in projects {
            if let ProjectDecision::Stale { reason } = policy.evaluate(proj, clock) {
                actions.push(Action {
                    id: format!("del-proj-{}", proj.id),
                    kind: ActionKind::DeleteProject,
                    target_id: proj.id.clone(),
                    target_name: proj.path_with_namespace.clone(),
                    description: format!(
                        "Permanently delete inactive project '{}'",
                        proj.path_with_namespace
                    ),
                    reasons: vec![reason],
                    metadata: HashMap::new(),
                });
            }
        }

        Plan::new(config.group.clone().unwrap_or_default(), "gitlab", actions)
    }

    pub fn plan_users(users: &[User], group: &str, config: &AppConfig, clock: &dyn Clock) -> Plan {
        let policy = UserPolicy::new(config.policies.user.clone());
        let mut actions = Vec::new();

        for user in users {
            if let UserDecision::Stale { reason } = policy.evaluate(user, clock) {
                actions.push(Action {
                    id: format!("rm-member-{}", user.id),
                    kind: ActionKind::RemoveGroupMember,
                    target_id: user.id.clone(),
                    target_name: user.username.clone(),
                    description: format!(
                        "Remove inactive member '{}' from group '{}'",
                        user.username, group
                    ),
                    reasons: vec![reason],
                    metadata: HashMap::new(),
                });
            }
        }

        Plan::new(group, "gitlab", actions)
    }

    pub fn plan_runner_tag_removal(runners: &[Runner], tag: &str, group: &str) -> Plan {
        let mut actions = Vec::new();

        for runner in runners {
            if let RunnerDecision::Eligible { reason } =
                RunnerTagPolicy::evaluate_tag_removal(runner, tag)
            {
                let mut new_tags = runner.tag_list.clone();
                new_tags.retain(|t| t != tag);

                let mut meta = HashMap::new();
                meta.insert("removed_tag".to_string(), tag.to_string());
                meta.insert("remaining_tags".to_string(), new_tags.join(","));

                actions.push(Action {
                    id: format!("tag-rm-runner-{}", runner.id),
                    kind: ActionKind::UpdateRunnerTags,
                    target_id: runner.id.clone(),
                    target_name: runner
                        .description
                        .clone()
                        .unwrap_or_else(|| runner.id.clone()),
                    description: format!("Remove tag '{}' from runner {}", tag, runner.id),
                    reasons: vec![reason],
                    metadata: meta,
                });
            }
        }

        Plan::new(group, "gitlab", actions)
    }
}
