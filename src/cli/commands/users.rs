use crate::app::Planner;
use crate::config::AppConfig;
use crate::domain::clock::DynClock;
use crate::policy::user::{UserDecision, UserPolicy};
use crate::provider::Provider;
use colored::Colorize;
use std::path::Path;
use tabled::{Table, Tabled};

#[derive(Tabled)]
struct UserTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Username")]
    username: String,
    #[tabled(rename = "Name")]
    name: String,
    #[tabled(rename = "Role")]
    role: String,
    #[tabled(rename = "Billable")]
    billable: String,
}

#[derive(Tabled)]
struct UserEvalTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Username")]
    username: String,
    #[tabled(rename = "Status")]
    status: String,
    #[tabled(rename = "Reason")]
    reason: String,
}

pub async fn list(provider: &dyn Provider, group: &str) -> anyhow::Result<()> {
    println!("Fetching members for group '{}'...", group);
    let members = provider.list_group_members(group).await?;

    if members.is_empty() {
        println!("No members found in group '{}'.", group);
        return Ok(());
    }

    let rows: Vec<UserTableRow> = members
        .iter()
        .map(|m| UserTableRow {
            id: m.id.clone(),
            username: m.username.clone(),
            name: m.name.clone(),
            role: m.access_level.to_display().to_string(),
            billable: if m.is_billable {
                "yes".to_string()
            } else {
                "no".to_string()
            },
        })
        .collect();

    println!("{}", Table::new(rows));
    println!("Total members: {}", members.len());
    Ok(())
}

pub async fn evaluate(
    provider: &dyn Provider,
    group: &str,
    config: &AppConfig,
    clock: DynClock,
) -> anyhow::Result<()> {
    println!("Evaluating members in group '{}'...", group);
    let members = provider.list_group_members(group).await?;
    let policy = UserPolicy::new(config.policies.user.clone());

    let mut rows = Vec::new();
    let mut stale_count = 0;

    for m in &members {
        let (status, reason) = match policy.evaluate(m, clock.as_ref()) {
            UserDecision::Stale { reason } => {
                stale_count += 1;
                ("STALE".red().bold().to_string(), reason)
            }
            UserDecision::Keep { reason } => ("KEEP".green().to_string(), reason),
        };

        rows.push(UserEvalTableRow {
            id: m.id.clone(),
            username: m.username.clone(),
            status,
            reason,
        });
    }

    println!("{}", Table::new(rows));
    println!(
        "\nSummary: {} total members, {} stale, {} to keep",
        members.len(),
        stale_count,
        members.len() - stale_count
    );

    Ok(())
}

pub async fn plan(
    provider: &dyn Provider,
    group: &str,
    config: &AppConfig,
    clock: DynClock,
    out_path: &Path,
) -> anyhow::Result<()> {
    let members = provider.list_group_members(group).await?;
    let plan = Planner::plan_users(&members, group, config, clock.as_ref());

    let json = serde_json::to_string_pretty(&plan)?;
    std::fs::write(out_path, json)?;

    println!(
        "Generated plan with {} action(s) written to {}",
        plan.actions.len(),
        out_path.display()
    );

    Ok(())
}
