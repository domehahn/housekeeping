use crate::app::Planner;
use crate::policy::runner::{RunnerDecision, RunnerTagPolicy};
use crate::provider::Provider;
use colored::Colorize;
use std::path::Path;
use tabled::{Table, Tabled};

#[derive(Tabled)]
struct RunnerTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Description")]
    description: String,
    #[tabled(rename = "Active")]
    active: String,
    #[tabled(rename = "Shared")]
    shared: String,
    #[tabled(rename = "Tags")]
    tags: String,
}

#[derive(Tabled)]
struct RunnerEvalTableRow {
    #[tabled(rename = "ID")]
    id: String,
    #[tabled(rename = "Status")]
    status: String,
    #[tabled(rename = "Reason")]
    reason: String,
}

pub async fn list(provider: &dyn Provider, group: &str) -> anyhow::Result<()> {
    println!("Fetching runners for group '{}'...", group);
    let runners = provider.list_runners(group).await?;

    if runners.is_empty() {
        println!("No runners found in group '{}'.", group);
        return Ok(());
    }

    let rows: Vec<RunnerTableRow> = runners
        .iter()
        .map(|r| RunnerTableRow {
            id: r.id.clone(),
            description: r.description.clone().unwrap_or_else(|| "none".into()),
            active: if r.active { "yes".into() } else { "no".into() },
            shared: if r.is_shared {
                "yes".into()
            } else {
                "no".into()
            },
            tags: r.tag_list.join(", "),
        })
        .collect();

    println!("{}", Table::new(rows));
    println!("Total runners: {}", runners.len());
    Ok(())
}

pub async fn evaluate(provider: &dyn Provider, group: &str, tag: &str) -> anyhow::Result<()> {
    println!(
        "Evaluating runners with tag '{}' in group '{}'...",
        tag, group
    );
    let runners = provider.list_runners(group).await?;

    let mut rows = Vec::new();
    let mut eligible_count = 0;

    for r in &runners {
        let (status, reason) = match RunnerTagPolicy::evaluate_tag_removal(r, tag) {
            RunnerDecision::Eligible { reason } => {
                eligible_count += 1;
                ("ELIGIBLE".yellow().bold().to_string(), reason)
            }
            RunnerDecision::Skip { reason } => ("SKIP".dimmed().to_string(), reason),
        };

        rows.push(RunnerEvalTableRow {
            id: r.id.clone(),
            status,
            reason,
        });
    }

    println!("{}", Table::new(rows));
    println!(
        "\nSummary: {} total runners, {} eligible for tag retirement",
        runners.len(),
        eligible_count
    );

    Ok(())
}

pub async fn plan(
    provider: &dyn Provider,
    group: &str,
    tag: &str,
    out_path: &Path,
) -> anyhow::Result<()> {
    let runners = provider.list_runners(group).await?;
    let plan = Planner::plan_runner_tag_removal(&runners, tag, group);

    let json = serde_json::to_string_pretty(&plan)?;
    std::fs::write(out_path, json)?;

    println!(
        "Generated runner tag plan with {} action(s) written to {}",
        plan.actions.len(),
        out_path.display()
    );

    Ok(())
}
