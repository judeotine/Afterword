use anyhow::{anyhow, Result};

pub const KEYCHAIN_SERVICE: &str = "com.afterword.app";

pub trait SecretStore: Send + Sync {
    fn get(&self, provider: &str) -> Result<Option<String>>;

    fn set(&self, provider: &str, value: &str) -> Result<()>;

    fn delete(&self, provider: &str) -> Result<()>;
}

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
        Self::entry(provider)?
            .set_password(value)
            .map_err(|e| anyhow!("failed to write keychain entry for '{}': {}", provider, e))
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

pub fn default_store() -> &'static dyn SecretStore {
    static STORE: KeychainSecretStore = KeychainSecretStore::new();
    &STORE
}

#[cfg(test)]
pub(crate) mod test_support {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;

    #[derive(Default)]
    pub struct MockSecretStore {
        entries: Mutex<HashMap<String, String>>,
        fail: bool,
    }

    impl MockSecretStore {
        pub fn new() -> Self {
            Self::default()
        }

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
