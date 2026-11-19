use crate::domain::runner::Runner;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RunnerDecision {
    Eligible { reason: String },
    Skip { reason: String },
}

pub struct RunnerTagPolicy;

impl RunnerTagPolicy {
    pub fn evaluate_tag_removal(runner: &Runner, tag: &str) -> RunnerDecision {
        if !runner.tag_list.contains(&tag.to_string()) {
            return RunnerDecision::Skip {
                reason: format!("Runner {} does not have tag '{}'", runner.id, tag),
            };
        }

        if runner.is_shared {
            return RunnerDecision::Skip {
                reason: "Shared/instance-wide runners cannot be mutated without instance admin privileges".to_string(),
            };
        }

        if runner.paused {
            return RunnerDecision::Skip {
                reason: "Runner is paused".to_string(),
            };
        }

        RunnerDecision::Eligible {
            reason: format!("Eligible for removing tag '{}'", tag),
        }
    }
}
