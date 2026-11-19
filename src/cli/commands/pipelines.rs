use crate::provider::Provider;
use colored::Colorize;

pub async fn inspect(provider: &dyn Provider, project_id: &str) -> anyhow::Result<()> {
    println!("Inspecting .gitlab-ci.yml for project '{}'...", project_id);
    let inspection = provider.inspect_pipeline_tags(project_id).await?;

    match inspection {
        Some(info) => {
            if info.jobs.is_empty() {
                println!("Found .gitlab-ci.yml, but no runner tags are defined in jobs.");
            } else {
                println!("{}", "Jobs and configured runner tags:".bold());
                for job in info.jobs {
                    println!("  - Job '{}': [{}]", job.job_name, job.tags.join(", "));
                }
            }
        }
        None => {
            println!("No .gitlab-ci.yml found in project '{}'.", project_id);
        }
    }

    Ok(())
}

pub async fn propose(
    provider: &dyn Provider,
    project_id: &str,
    old_tag: &str,
    new_tag: &str,
) -> anyhow::Result<()> {
    println!(
        "Proposing replacement of runner tag '{}' -> '{}' for project '{}'...",
        old_tag, new_tag, project_id
    );

    let mr_url = provider
        .propose_pipeline_tag_change(project_id, old_tag, new_tag)
        .await?;

    println!(
        "Successfully opened Merge Request: {}",
        mr_url.green().bold()
    );
    Ok(())
}
