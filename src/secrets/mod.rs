use keyring::Entry;
use thiserror::Error;

const KEYRING_SERVICE: &str = "scm-cleaner";

#[derive(Debug, Error)]
pub enum SecretError {
    #[error("Environment variable '{0}' is not set or empty")]
    EnvNotFound(String),
    #[error("Keyring entry for '{0}' not found")]
    KeyringNotFound(String),
    #[error("Keyring error: {0}")]
    KeyringError(String),
}

pub struct SecretResolver;

impl SecretResolver {
    pub fn resolve_env(var_name: &str) -> Result<String, SecretError> {
        match std::env::var(var_name) {
            Ok(val) if !val.trim().is_empty() => Ok(val.trim().to_string()),
            _ => Err(SecretError::EnvNotFound(var_name.to_string())),
        }
    }

    pub fn get_keychain_token(target: &str) -> Result<String, SecretError> {
        let entry = Entry::new(KEYRING_SERVICE, target)
            .map_err(|e| SecretError::KeyringError(e.to_string()))?;
        entry
            .get_password()
            .map_err(|_| SecretError::KeyringNotFound(target.to_string()))
    }

    pub fn set_keychain_token(target: &str, token: &str) -> Result<(), SecretError> {
        let entry = Entry::new(KEYRING_SERVICE, target)
            .map_err(|e| SecretError::KeyringError(e.to_string()))?;
        entry
            .set_password(token)
            .map_err(|e| SecretError::KeyringError(e.to_string()))
    }

    pub fn delete_keychain_token(target: &str) -> Result<(), SecretError> {
        let entry = Entry::new(KEYRING_SERVICE, target)
            .map_err(|e| SecretError::KeyringError(e.to_string()))?;
        entry
            .delete_credential()
            .map_err(|e| SecretError::KeyringError(e.to_string()))
    }
}
