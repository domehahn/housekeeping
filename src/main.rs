use clap::Parser;
use scm_cleaner::adapters::GitLabAdapter;
use scm_cleaner::cli::args::{
    AuthCommands, Cli, Commands, PipelineCommands, ProjectCommands, ProviderCommands,
    RunnerCommands, UserCommands,
};
use scm_cleaner::cli::commands;
use scm_cleaner::config::AppConfig;
use scm_cleaner::domain::clock::{DynClock, SystemClock};
use scm_cleaner::provider::Provider;
use scm_cleaner::secrets::SecretResolver;
use std::sync::Arc;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    // 1. Handle commands that don't need credentials
    match &cli.command {
        Commands::Version => {
            commands::version::show();
            return Ok(());
        }
        Commands::Auth { command } => match command {
            AuthCommands::Login { token } => {
                return commands::auth::login(&cli.gitlab_url, token.clone());
            }
            AuthCommands::Status => {
                return commands::auth::status(&cli.gitlab_url);
            }
            AuthCommands::Logout => {
                return commands::auth::logout(&cli.gitlab_url);
            }
        },
        _ => {}
    }

    // 2. Load optional configuration file
    let mut config = if let Some(cfg_path) = &cli.config {
        AppConfig::load_from_file(cfg_path)?
    } else {
        AppConfig::default()
    };

    if let Some(grp) = &cli.group {
        config.group = Some(grp.clone());
    }
    if cli.recursive {
        config.recursive = true;
    }

    // 3. Resolve API token: First try configured env var, then fallback to OS keychain
    let token = match SecretResolver::resolve_env(&cli.token_env) {
        Ok(t) => t,
        Err(_) => match SecretResolver::get_keychain_token(&cli.gitlab_url) {
            Ok(t) => t,
            Err(_) => {
                anyhow::bail!(
                    "Could not resolve GitLab API token from environment variable '{}' or OS keychain for '{}'. \
                    Please set the environment variable or run 'scm-cleaner auth login'.",
                    cli.token_env,
                    cli.gitlab_url
                );
            }
        },
    };

    // 4. Instantiate provider adapter
    let provider = GitLabAdapter::new(&cli.gitlab_url, &token)?;
    let clock: DynClock = Arc::new(SystemClock);

    // 5. Dispatch commands
    match cli.command {
        Commands::Version | Commands::Auth { .. } => unreachable!(),

        Commands::Doctor => {
            commands::doctor::run(&provider, config.group.as_deref()).await?;
        }

        Commands::Provider { command } => match command {
            ProviderCommands::Info => {
                let info = provider.info().await?;
                println!("{}", info);
            }
        },

        Commands::Projects { command } => {
            let group = config
                .group
                .as_deref()
                .ok_or_else(|| anyhow::anyhow!("Target group is required. Specify via --group"))?;
            match command {
                ProjectCommands::List => {
                    commands::projects::list(&provider, group, config.recursive).await?;
                }
                ProjectCommands::Evaluate => {
                    commands::projects::evaluate(
                        &provider,
                        group,
                        config.recursive,
                        &config,
                        clock,
                    )
                    .await?;
                }
                ProjectCommands::Plan { out } => {
                    commands::projects::plan(
                        &provider,
                        group,
                        config.recursive,
                        &config,
                        clock,
                        &out,
                    )
                    .await?;
                }
            }
        }

        Commands::Users { command } => {
            let group = config
                .group
                .as_deref()
                .ok_or_else(|| anyhow::anyhow!("Target group is required. Specify via --group"))?;
            match command {
                UserCommands::List => {
                    commands::users::list(&provider, group).await?;
                }
                UserCommands::Evaluate => {
                    commands::users::evaluate(&provider, group, &config, clock).await?;
                }
                UserCommands::Plan { out } => {
                    commands::users::plan(&provider, group, &config, clock, &out).await?;
                }
            }
        }

        Commands::Runners { command } => {
            let group = config
                .group
                .as_deref()
                .ok_or_else(|| anyhow::anyhow!("Target group is required. Specify via --group"))?;
            match command {
                RunnerCommands::List => {
                    commands::runners::list(&provider, group).await?;
                }
                RunnerCommands::Evaluate { tag } => {
                    commands::runners::evaluate(&provider, group, &tag).await?;
                }
                RunnerCommands::Plan { tag, out } => {
                    commands::runners::plan(&provider, group, &tag, &out).await?;
                }
            }
        }

        Commands::Pipelines { command } => match command {
            PipelineCommands::Inspect { project } => {
                let project_id = project
                    .or(config.group)
                    .ok_or_else(|| anyhow::anyhow!("Project path or ID is required"))?;
                commands::pipelines::inspect(&provider, &project_id).await?;
            }
            PipelineCommands::Propose {
                project,
                old_tag,
                new_tag,
            } => {
                commands::pipelines::propose(&provider, &project, &old_tag, &new_tag).await?;
            }
        },

        Commands::Execute(args) => {
            commands::execute::run(
                &provider,
                &args.plan,
                args.dry_run,
                args.force,
                &cli.audit_file,
                config.safety,
            )
            .await?;
        }
    }

    Ok(())
}
