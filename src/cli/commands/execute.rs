use crate::app::Executor;
use crate::audit::AuditLogger;
use crate::config::SafetyConfig;
use crate::domain::action::Plan;
use crate::provider::Provider;
use colored::Colorize;
use std::io::{self, Write};
use std::path::Path;

pub async fn run(
    provider: &dyn Provider,
    plan_path: &Path,
    dry_run: bool,
    force: bool,
    audit_path: &Path,
    safety: SafetyConfig,
) -> anyhow::Result<()> {
    if !plan_path.exists() {
        anyhow::bail!("Plan file not found: {}", plan_path.display());
    }

    let content = std::fs::read_to_string(plan_path)?;
    let plan: Plan = serde_json::from_str(&content)?;

    println!("{}", "=== SCM CLEANER PLAN EXECUTION ===".bold());
    println!("Target Group: {}", plan.group);
    println!("Total Actions: {}", plan.actions.len());
    println!(
        "Mode: {}",
        if dry_run {
            "DRY RUN (no changes)".yellow().bold()
        } else {
            "LIVE EXECUTION".red().bold()
        }
    );

    if plan.actions.is_empty() {
        println!("No actions to execute.");
        return Ok(());
    }

    for (i, action) in plan.actions.iter().enumerate() {
        println!(
            "  [{}] {:?} on {} ({})",
            i + 1,
            action.kind,
            action.target_name.cyan(),
            action.description
        );
    }

    if !dry_run && !force {
        print!(
            "\nAre you sure you want to proceed with executing these destructive actions? [y/N]: "
        );
        io::stdout().flush()?;
        let mut input = String::new();
        io::stdin().read_line(&mut input)?;

        if input.trim().to_lowercase() != "y" {
            println!("Execution aborted by user.");
            return Ok(());
        }
    }

    let logger = AuditLogger::new(audit_path);
    let executor = Executor::new(provider, Some(&logger), safety);

    let summary = executor.execute(&plan, dry_run).await?;

    println!("\n{}", "=== EXECUTION SUMMARY ===".bold());
    println!("Total:     {}", summary.total);
    println!("Succeeded: {}", summary.succeeded.to_string().green());
    println!("Failed:    {}", summary.failed.to_string().red());
    println!("Audit log: {}", audit_path.display());

    Ok(())
}
