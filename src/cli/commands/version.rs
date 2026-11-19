use colored::Colorize;

pub fn show() {
    println!("{}", "scm-cleaner (Rust Edition)".bold());
    println!("Version:    {}", env!("CARGO_PKG_VERSION"));
    println!(
        "Platform:   {}-{}",
        std::env::consts::OS,
        std::env::consts::ARCH
    );
    println!("Repository: {}", env!("CARGO_PKG_REPOSITORY"));
}
