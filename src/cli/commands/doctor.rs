use crate::provider::Provider;
use colored::Colorize;

pub async fn run(provider: &dyn Provider, group: Option<&str>) -> anyhow::Result<()> {
    println!("{}", "=== SCM CLEANER DOCTOR ===".bold());

    // 1. Provider Connectivity
    print!("Checking provider connectivity... ");
    match provider.info().await {
        Ok(info) => println!("{} ({})", "OK".green().bold(), info),
        Err(e) => {
            println!("{} ({})", "FAILED".red().bold(), e);
            anyhow::bail!("Provider check failed");
        }
    }

    // 2. Group Permissions
    if let Some(grp) = group {
        print!("Checking access to group '{}'... ", grp);
        match provider.list_projects(grp, false).await {
            Ok(projs) => println!(
                "{} (Able to read projects, found {})",
                "OK".green().bold(),
                projs.len()
            ),
            Err(e) => println!("{} ({})", "WARNING".yellow().bold(), e),
        }
    }

    println!("\n{}", "All critical health checks passed!".green().bold());
    Ok(())
}
