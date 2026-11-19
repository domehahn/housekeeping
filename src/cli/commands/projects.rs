use crate::app::Planner;
use crate::config::AppConfig;
use crate::domain::clock::DynClock;
use crate::policy::project::{ProjectDecision, ProjectPolicy};
use crate::provider::Provider;
use colored::Colorize;
use std::path::Path;
use tabled::{Table, Tabled};

#[derive(Tabled)]
struct ProjectTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Path")]
    path: String,
    #[tabled(rename = "Archived")]
    archived: String,
    #[tabled(rename = "Last Activity")]
    last_activity: String,
}

#[derive(Tabled)]
struct ProjectEvalTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Path")]
    path: String,
    #[tabled(rename = "Status")]
    status: String,
    #[tabled(rename = "Reason")]
    reason: String,
}

pub async fn list(provider: &dyn Provider, group: &str, recursive: bool) -> anyhow::Result<()> {
    println!(
        "Fetching projects for group '{}' (recursive: {})...",
        group, recursive
    );
    let projects = provider.list_projects(group, recursive).await?;

    if projects.is_empty() {
        println!("No projects found in group '{}'.", group);
        return Ok(());
    }

    let rows: Vec<ProjectTableRow> = projects
        .iter()
        .map(|p| ProjectTableRow {
            id: p.id.clone(),
            path: p.path_with_namespace.clone(),
            archived: if p.archived {
                "yes".to_string()
            } else {
                "no".to_string()
            },
            last_activity: p
                .last_activity_at
                .map(|t| t.format("%Y-%m-%d").to_string())
                .unwrap_or_else(|| "never / unknown".into()),
        })
        .collect();

    println!("{}", Table::new(rows));
    println!("Total projects: {}", projects.len());
    Ok(())
}

pub async fn evaluate(
    provider: &dyn Provider,
    group: &str,
    recursive: bool,
    config: &AppConfig,
    clock: DynClock,
) -> anyhow::Result<()> {
    println!("Evaluating projects in group '{}'...", group);
    let projects = provider.list_projects(group, recursive).await?;
    let policy = ProjectPolicy::new(config.policies.project.clone());

    let mut rows = Vec::new();
    let mut stale_count = 0;

    for p in &projects {
        let (status, reason) = match policy.evaluate(p, clock.as_ref()) {
            ProjectDecision::Stale { reason } => {
                stale_count += 1;
                ("STALE".red().bold().to_string(), reason)
            }
            ProjectDecision::Keep { reason } => ("KEEP".green().to_string(), reason),
        };

        rows.push(ProjectEvalTableRow {
            id: p.id.clone(),
            path: p.path_with_namespace.clone(),
            status,
            reason,
        });
    }

    println!("{}", Table::new(rows));
    println!(
        "\nSummary: {} total, {} stale, {} to keep",
        projects.len(),
        stale_count,
        projects.len() - stale_count
    );

    Ok(())
}

pub async fn plan(
    provider: &dyn Provider,
    group: &str,
    recursive: bool,
    config: &AppConfig,
    clock: DynClock,
    out_path: &Path,
) -> anyhow::Result<()> {
    let projects = provider.list_projects(group, recursive).await?;
    let plan = Planner::plan_projects(&projects, config, clock.as_ref());

    let json = serde_json::to_string_pretty(&plan)?;
    std::fs::write(out_path, json)?;

    println!(
        "Generated plan with {} action(s) written to {}",
        plan.actions.len(),
        out_path.display()
    );

    Ok(())
}
