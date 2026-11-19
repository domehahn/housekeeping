use crate::app::safety::SafetyGuard;
use crate::audit::{AuditLogger, AuditRecord};
use crate::config::SafetyConfig;
use crate::domain::action::{ActionKind, Plan};
use crate::provider::Provider;
use chrono::Utc;

pub struct ExecutionSummary {
    pub total: usize,
    pub succeeded: usize,
    pub failed: usize,
    pub dry_run: bool,
}

pub struct Executor<'a> {
    provider: &'a dyn Provider,
    audit_logger: Option<&'a AuditLogger>,
    safety: SafetyConfig,
}

impl<'a> Executor<'a> {
    pub fn new(
        provider: &'a dyn Provider,
        audit_logger: Option<&'a AuditLogger>,
        safety: SafetyConfig,
    ) -> Self {
        Self {
            provider,
            audit_logger,
            safety,
        }
    }

    pub async fn execute(&self, plan: &Plan, dry_run: bool) -> anyhow::Result<ExecutionSummary> {
        // Enforce safety guards before proceeding
        SafetyGuard::check_plan(plan, &self.safety)?;

        let mut summary = ExecutionSummary {
            total: plan.actions.len(),
            succeeded: 0,
            failed: 0,
            dry_run,
        };

        for action in &plan.actions {
            let mut record = AuditRecord {
                timestamp: Utc::now(),
                action_id: action.id.clone(),
                action_kind: format!("{:?}", action.kind),
                target_id: action.target_id.clone(),
                target_name: action.target_name.clone(),
                dry_run,
                success: false,
                error: None,
            };

            if dry_run {
                println!("[DRY RUN] Would execute: {}", action.description);
                record.success = true;
                summary.succeeded += 1;
            } else {
                println!("[EXECUTE] Executing: {}", action.description);
                let result = match action.kind {
                    ActionKind::DeleteProject => {
                        self.provider.delete_project(&action.target_id).await
                    }
                    ActionKind::RemoveGroupMember => {
                        self.provider
                            .remove_group_member(&plan.group, &action.target_id)
                            .await
                    }
                    ActionKind::UpdateRunnerTags => {
                        let remaining_str = action
                            .metadata
                            .get("remaining_tags")
                            .cloned()
                            .unwrap_or_default();
                        let tags: Vec<String> = if remaining_str.is_empty() {
                            vec![]
                        } else {
                            remaining_str
                                .split(',')
                                .map(|s| s.trim().to_string())
                                .collect()
                        };
                        self.provider
                            .update_runner_tags(&action.target_id, tags)
                            .await
                    }
                    ActionKind::ArchiveProject => {
                        println!("Archive not directly supported via API; skipping");
                        Ok(())
                    }
                    ActionKind::ProposePipelineTagChange => {
                        println!("Pipeline tag proposal action triggered");
                        Ok(())
                    }
                };

                match result {
                    Ok(_) => {
                        record.success = true;
                        summary.succeeded += 1;
                    }
                    Err(e) => {
                        eprintln!("[ERROR] Action {} failed: {}", action.id, e);
                        record.error = Some(e.to_string());
                        summary.failed += 1;
                    }
                }
            }

            if let Some(logger) = self.audit_logger {
                let _ = logger.log(&record);
            }
        }

        Ok(summary)
    }
}
