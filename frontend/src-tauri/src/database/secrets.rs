//! OS keychain storage for provider API keys.
//!
//! API keys used to live in plaintext columns of the SQLite settings database.
//! They are now stored in the platform secret store (macOS Keychain, Windows
//! Credential Manager, Linux Secret Service) under the service name
//! `com.afterword.app`, with the provider id as the account name.
//!
//! The [`SecretStore`] trait exists so callers can be tested without touching a
//! real keychain (which is unavailable in CI and headless Linux).

use anyhow::{anyhow, Result};

/// Keychain service name shared by every stored secret.
pub const KEYCHAIN_SERVICE: &str = "com.afterword.app";

/// Abstraction over the platform secret store.
pub trait SecretStore: Send + Sync {
    /// Read the secret for `provider`, if one is stored.
    fn get(&self, provider: &str) -> Result<Option<String>>;

    /// Store (or replace) the secret for `provider`.
    fn set(&self, provider: &str, value: &str) -> Result<()>;

    /// Remove the secret for `provider`. Deleting a missing entry is not an error.
    fn delete(&self, provider: &str) -> Result<()>;
}

/// The real secret store, backed by the `keyring` crate.
#[derive(Debug, Default, Clone, Copy)]
pub struct KeychainSecretStore;

impl KeychainSecretStore {
    pub const fn new() -> Self {
        Self
    }

    fn entry(provider: &str) -> Result<keyring::Entry> {
        keyring::Entry::new(KEYCHAIN_SERVICE, provider)
            .map_err(|e| anyhow!("failed to open keychain entry for '{}': {}", provider, e))
    }
}

impl SecretStore for KeychainSecretStore {
    fn get(&self, provider: &str) -> Result<Option<String>> {
        match Self::entry(provider)?.get_password() {
            Ok(value) => Ok(Some(value)),
            Err(keyring::Error::NoEntry) => Ok(None),
            Err(e) => Err(anyhow!(
                "failed to read keychain entry for '{}': {}",
                provider,
                e
            )),
        }
    }

    fn set(&self, provider: &str, value: &str) -> Result<()> {
        Self::entry(provider)?.set_password(value).map_err(|e| {
            anyhow!(
                "failed to write keychain entry for '{}': {}",
                provider,
                e
            )
        })
    }

    fn delete(&self, provider: &str) -> Result<()> {
        match Self::entry(provider)?.delete_credential() {
            Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
            Err(e) => Err(anyhow!(
                "failed to delete keychain entry for '{}': {}",
                provider,
                e
            )),
        }
    }
}

/// The process-wide secret store used by the settings repository.
pub fn default_store() -> &'static dyn SecretStore {
    static STORE: KeychainSecretStore = KeychainSecretStore::new();
    &STORE
}

#[cfg(test)]
pub(crate) mod test_support {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;

    /// In-memory [`SecretStore`] used by tests. It can be told to fail every
    /// operation, which is how a headless Linux box without a Secret Service
    /// daemon behaves.
    #[derive(Default)]
    pub struct MockSecretStore {
        entries: Mutex<HashMap<String, String>>,
        fail: bool,
    }

    impl MockSecretStore {
        pub fn new() -> Self {
            Self::default()
        }

        /// A store where every operation fails (no keychain available).
        pub fn unavailable() -> Self {
            Self {
                entries: Mutex::new(HashMap::new()),
                fail: true,
            }
        }

        pub fn peek(&self, provider: &str) -> Option<String> {
            self.entries.lock().unwrap().get(provider).cloned()
        }
    }

    impl SecretStore for MockSecretStore {
        fn get(&self, provider: &str) -> Result<Option<String>> {
            if self.fail {
                return Err(anyhow!("no secret service available"));
            }
            Ok(self.entries.lock().unwrap().get(provider).cloned())
        }

        fn set(&self, provider: &str, value: &str) -> Result<()> {
            if self.fail {
                return Err(anyhow!("no secret service available"));
            }
            self.entries
                .lock()
                .unwrap()
                .insert(provider.to_string(), value.to_string());
            Ok(())
        }

        fn delete(&self, provider: &str) -> Result<()> {
            if self.fail {
                return Err(anyhow!("no secret service available"));
            }
            self.entries.lock().unwrap().remove(provider);
            Ok(())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::test_support::MockSecretStore;
    use super::*;

    #[test]
    fn mock_store_round_trips_values() {
        let store = MockSecretStore::new();
        assert_eq!(store.get("openai").unwrap(), None);

        store.set("openai", "sk-test").unwrap();
        assert_eq!(store.get("openai").unwrap().as_deref(), Some("sk-test"));

        store.delete("openai").unwrap();
        assert_eq!(store.get("openai").unwrap(), None);
    }

    #[test]
    fn unavailable_store_reports_errors() {
        let store = MockSecretStore::unavailable();
        assert!(store.set("openai", "sk-test").is_err());
        assert!(store.get("openai").is_err());
        assert!(store.delete("openai").is_err());
    }

    /// Touches the real OS keychain, so it is ignored by default. Run with
    /// `cargo test -p afterword --lib database::secrets -- --ignored` and verify
    /// with `security find-generic-password -s com.afterword.app` on macOS.
    #[test]
    #[ignore = "writes to the real OS keychain and may prompt for access"]
    fn keychain_store_round_trips_values() {
        let store = KeychainSecretStore::new();
        let provider = "afterword-test-provider";

        store.set(provider, "sk-keychain-test").unwrap();
        assert_eq!(
            store.get(provider).unwrap().as_deref(),
            Some("sk-keychain-test")
        );

        store.delete(provider).unwrap();
        assert_eq!(store.get(provider).unwrap(), None);
    }
}
