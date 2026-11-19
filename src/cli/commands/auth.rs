use crate::secrets::SecretResolver;
use colored::Colorize;
use std::io::{self, Write};

pub fn login(target: &str, token_arg: Option<String>) -> anyhow::Result<()> {
    let token = match token_arg {
        Some(t) => t,
        None => {
            print!("Enter GitLab Personal Access Token for '{}': ", target);
            io::stdout().flush()?;
            let mut input = String::new();
            io::stdin().read_line(&mut input)?;
            input.trim().to_string()
        }
    };

    if token.is_empty() {
        anyhow::bail!("Token cannot be empty");
    }

    SecretResolver::set_keychain_token(target, &token)?;
    println!(
        "{}",
        format!(
            "Successfully stored token for '{}' in OS native keychain.",
            target
        )
        .green()
        .bold()
    );
    Ok(())
}

pub fn status(target: &str) -> anyhow::Result<()> {
    match SecretResolver::get_keychain_token(target) {
        Ok(_) => {
            println!(
                "Keychain status for '{}': {}",
                target,
                "AUTHENTICATED (Token present in keychain)".green().bold()
            );
        }
        Err(_) => {
            println!(
                "Keychain status for '{}': {}",
                target,
                "NOT AUTHENTICATED (No token found in keychain)".yellow()
            );
        }
    }
    Ok(())
}

pub fn logout(target: &str) -> anyhow::Result<()> {
    SecretResolver::delete_keychain_token(target)?;
    println!(
        "{}",
        format!("Removed token for '{}' from OS native keychain.", target).green()
    );
    Ok(())
}
