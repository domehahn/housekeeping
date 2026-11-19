use crate::config::SafetyConfig;
use crate::domain::action::{ActionKind, Plan};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum SafetyError {
    #[error(
        "Plan exceeds maximum allowed destructive operations: {count} actions > limit of {limit}"
    )]
    BlastRadiusExceeded { count: usize, limit: usize },
    #[error("No actions specified in plan")]
    EmptyPlan,
}

pub struct SafetyGuard;

impl SafetyGuard {
    pub fn check_plan(plan: &Plan, config: &SafetyConfig) -> Result<(), SafetyError> {
        let destructive_count = plan
            .actions
            .iter()
            .filter(|a| {
                matches!(
                    a.kind,
                    ActionKind::DeleteProject | ActionKind::RemoveGroupMember
                )
            })
            .count();

        if destructive_count > config.max_deletions {
            return Err(SafetyError::BlastRadiusExceeded {
                count: destructive_count,
                limit: config.max_deletions,
            });
        }

        Ok(())
    }
}
