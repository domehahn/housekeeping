use clap::{Args, Parser, Subcommand};
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(
    name = "scm-cleaner",
    author = "Dominik Hahn",
    version,
    about = "Safe, policy-driven CLI to discover, plan, and clean up stale SCM projects, users, runner tags, and CI pipelines.",
    long_about = None
)]
pub struct Cli {
    #[arg(
        long,
        env = "GITLAB_URL",
        default_value = "https://gitlab.com",
        help = "GitLab base URL (default: https://gitlab.com)"
    )]
    pub gitlab_url: String,

    #[arg(
        long,
        default_value = "GITLAB_TOKEN",
        help = "Environment variable holding the GitLab API token"
    )]
    pub token_env: String,

    #[arg(short, long, help = "Target GitLab group path or ID")]
    pub group: Option<String>,

    #[arg(short, long, help = "Include all subgroups recursively")]
    pub recursive: bool,

    #[arg(short, long, help = "Path to optional YAML configuration file")]
    pub config: Option<PathBuf>,

    #[arg(
        long,
        default_value = "audit.jsonl",
        help = "Audit log file path for execution records"
    )]
    pub audit_file: PathBuf,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Debug, Subcommand)]
pub enum Commands {
    #[command(about = "Display information about the connected SCM provider")]
    Provider {
        #[command(subcommand)]
        command: ProviderCommands,
    },

    #[command(about = "Discover, inspect, evaluate, and plan project cleanups")]
    Projects {
        #[command(subcommand)]
        command: ProjectCommands,
    },

    #[command(about = "Inspect and evaluate GitLab group members and billable seats")]
    Users {
        #[command(subcommand)]
        command: UserCommands,
    },

    #[command(about = "Manage, inspect, and evaluate GitLab CI runners and tags")]
    Runners {
        #[command(subcommand)]
        command: RunnerCommands,
    },

    #[command(about = "Inspect and propose GitLab CI pipeline configuration changes")]
    Pipelines {
        #[command(subcommand)]
        command: PipelineCommands,
    },

    #[command(about = "Execute an approved cleanup plan")]
    Execute(ExecuteArgs),

    #[command(about = "Manage authentication tokens in the OS native keychain")]
    Auth {
        #[command(subcommand)]
        command: AuthCommands,
    },

    #[command(about = "Verify environment, permissions, and provider connectivity")]
    Doctor,

    #[command(about = "Show build and version information")]
    Version,
}

#[derive(Debug, Subcommand)]
pub enum ProviderCommands {
    #[command(about = "Show provider version and instance connectivity")]
    Info,
}

#[derive(Debug, Subcommand)]
pub enum ProjectCommands {
    #[command(about = "List all projects in the selected group")]
    List,
    #[command(about = "Evaluate projects against inactivity and protection policies")]
    Evaluate,
    #[command(about = "Generate a reviewable JSON plan file for stale projects")]
    Plan {
        #[arg(
            short,
            long,
            default_value = "projects-plan.json",
            help = "Output plan file path"
        )]
        out: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    #[command(about = "List all members of the selected group")]
    List,
    #[command(about = "Evaluate group members against inactivity policies")]
    Evaluate,
    #[command(about = "Generate a reviewable JSON plan file for stale members")]
    Plan {
        #[arg(
            short,
            long,
            default_value = "users-plan.json",
            help = "Output plan file path"
        )]
        out: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
pub enum RunnerCommands {
    #[command(about = "List runners registered in the group")]
    List,
    #[command(about = "Evaluate runners with specific tag")]
    Evaluate {
        #[arg(long, help = "Tag to evaluate for retirement")]
        tag: String,
    },
    #[command(about = "Generate a plan to remove a retired tag from eligible runners")]
    Plan {
        #[arg(long, help = "Tag to remove from runners")]
        tag: String,
        #[arg(
            short,
            long,
            default_value = "runners-plan.json",
            help = "Output plan file path"
        )]
        out: PathBuf,
    },
}

#[derive(Debug, Subcommand)]
pub enum PipelineCommands {
    #[command(about = "Inspect .gitlab-ci.yml files for runner tags")]
    Inspect {
        #[arg(long, help = "Specific project path or ID (optional)")]
        project: Option<String>,
    },
    #[command(about = "Propose a runner tag replacement via Merge Request")]
    Propose {
        #[arg(long, help = "Project path or ID")]
        project: String,
        #[arg(long, help = "Old tag to be replaced")]
        old_tag: String,
        #[arg(long, help = "New replacement tag")]
        new_tag: String,
    },
}

#[derive(Debug, Args)]
pub struct ExecuteArgs {
    #[arg(short, long, help = "Path to the plan file to execute")]
    pub plan: PathBuf,

    #[arg(long, help = "Simulate actions without mutating any state")]
    pub dry_run: bool,

    #[arg(long, help = "Bypass interactive confirmation prompt")]
    pub force: bool,
}

#[derive(Debug, Subcommand)]
pub enum AuthCommands {
    #[command(about = "Store a GitLab API token in the OS native keychain")]
    Login {
        #[arg(long, help = "GitLab API token (or read interactively if omitted)")]
        token: Option<String>,
    },
    #[command(about = "Check authentication status and token presence")]
    Status,
    #[command(about = "Remove the stored token from the OS keychain")]
    Logout,
}
