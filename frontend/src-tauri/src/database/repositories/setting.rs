use crate::database::models::{Setting, TranscriptSetting};
use crate::database::secrets::{default_store, SecretStore};
use crate::summary::CustomOpenAIConfig;
use sqlx::SqlitePool;

/// Provider id used for the custom OpenAI-compatible endpoint's key.
const CUSTOM_OPENAI_PROVIDER: &str = "custom-openai";

/// Legacy SQLite column that used to hold the API key for a summary provider.
/// `Ok(None)` means the provider needs no key at all.
fn api_key_column(provider: &str) -> std::result::Result<Option<&'static str>, sqlx::Error> {
    match provider {
        "openai" => Ok(Some("openaiApiKey")),
        "claude" => Ok(Some("anthropicApiKey")),
        "ollama" => Ok(Some("ollamaApiKey")),
        "groq" => Ok(Some("groqApiKey")),
        "openrouter" => Ok(Some("openRouterApiKey")),
        "builtin-ai" => Ok(None), // No API key needed
        _ => Err(sqlx::Error::Protocol(
            format!("Invalid provider: {}", provider).into(),
        )),
    }
}

/// Legacy SQLite column that used to hold the API key for a transcript provider.
/// `Ok(None)` means the provider needs no key at all.
fn transcript_api_key_column(provider: &str) -> std::result::Result<Option<&'static str>, sqlx::Error> {
    match provider {
        "localWhisper" => Ok(Some("whisperApiKey")),
        "deepgram" => Ok(Some("deepgramApiKey")),
        "elevenLabs" => Ok(Some("elevenLabsApiKey")),
        "groq" => Ok(Some("groqApiKey")),
        "openai" => Ok(Some("openaiApiKey")),
        "parakeet" => Ok(None), // Runs locally, no API key
        _ => Err(sqlx::Error::Protocol(
            format!("Invalid provider: {}", provider).into(),
        )),
    }
}

/// Keychain account name for a transcript provider's key. Namespaced because
/// several provider ids (groq, openai) are shared with the summary settings.
fn transcript_secret_account(provider: &str) -> String {
    format!("transcript:{}", provider)
}

/// Clear a legacy plaintext column once the key lives in the keychain.
async fn clear_legacy_column(
    pool: &SqlitePool,
    table: &str,
    column: &str,
) -> std::result::Result<(), sqlx::Error> {
    let query = format!("UPDATE {} SET \"{}\" = NULL WHERE id = '1'", table, column);
    sqlx::query(&query).execute(pool).await?;
    Ok(())
}

/// Read a legacy plaintext column, tolerating a missing row, a NULL value and a
/// blank string (all of which mean "no key stored").
async fn read_legacy_column(
    pool: &SqlitePool,
    table: &str,
    column: &str,
) -> std::result::Result<Option<String>, sqlx::Error> {
    let query = format!(
        "SELECT \"{}\" FROM {} WHERE id = '1' LIMIT 1",
        column, table
    );
    let value: Option<Option<String>> = sqlx::query_scalar(&query).fetch_optional(pool).await?;
    Ok(value.flatten().filter(|k| !k.trim().is_empty()))
}

/// Write the legacy settings column (fallback when no keychain is available).
async fn write_legacy_settings_column(
    pool: &SqlitePool,
    column: &str,
    api_key: &str,
) -> std::result::Result<(), sqlx::Error> {
    let query = format!(
        r#"
            INSERT INTO settings (id, provider, model, whisperModel, "{}")
            VALUES ('1', 'openai', 'gpt-4o-2024-11-20', 'large-v3', $1)
            ON CONFLICT(id) DO UPDATE SET
                "{}" = $1
            "#,
        column, column
    );
    sqlx::query(&query).bind(api_key).execute(pool).await?;
    Ok(())
}

/// Write the legacy transcript_settings column (fallback when no keychain is available).
async fn write_legacy_transcript_column(
    pool: &SqlitePool,
    column: &str,
    api_key: &str,
) -> std::result::Result<(), sqlx::Error> {
    let query = format!(
        r#"
            INSERT INTO transcript_settings (id, provider, model, "{}")
            VALUES ('1', 'parakeet', '{}', $1)
            ON CONFLICT(id) DO UPDATE SET
                "{}" = $1
            "#,
        column, crate::config::DEFAULT_PARAKEET_MODEL, column
    );
    sqlx::query(&query).bind(api_key).execute(pool).await?;
    Ok(())
}

#[derive(serde::Deserialize, Debug)]
pub struct SaveModelConfigRequest {
    pub provider: String,
    pub model: String,
    #[serde(rename = "whisperModel")]
    pub whisper_model: String,
    #[serde(rename = "apiKey")]
    pub api_key: Option<String>,
    #[serde(rename = "ollamaEndpoint")]
    pub ollama_endpoint: Option<String>,
}

#[derive(serde::Deserialize, Debug)]
pub struct SaveTranscriptConfigRequest {
    pub provider: String,
    pub model: String,
    #[serde(rename = "apiKey")]
    pub api_key: Option<String>,
}

pub struct SettingsRepository;

// Transcript providers: localWhisper, deepgram, elevenLabs, groq, openai
// Summary providers: openai, claude, ollama, groq, added openrouter
// NOTE: Handle data exclusion in the higher layer as this is database abstraction layer(using SELECT *)

impl SettingsRepository {
    pub async fn get_model_config(
        pool: &SqlitePool,
    ) -> std::result::Result<Option<Setting>, sqlx::Error> {
        let setting = sqlx::query_as::<_, Setting>("SELECT * FROM settings LIMIT 1")
            .fetch_optional(pool)
            .await?;
        Ok(setting)
    }

    pub async fn save_model_config(
        pool: &SqlitePool,
        provider: &str,
        model: &str,
        whisper_model: &str,
        ollama_endpoint: Option<&str>,
    ) -> std::result::Result<(), sqlx::Error> {
        // Using id '1' for backward compatibility
        sqlx::query(
            r#"
            INSERT INTO settings (id, provider, model, whisperModel, ollamaEndpoint)
            VALUES ('1', $1, $2, $3, $4)
            ON CONFLICT(id) DO UPDATE SET
                provider = excluded.provider,
                model = excluded.model,
                whisperModel = excluded.whisperModel,
                ollamaEndpoint = excluded.ollamaEndpoint
            "#,
        )
        .bind(provider)
        .bind(model)
        .bind(whisper_model)
        .bind(ollama_endpoint)
        .execute(pool)
        .await?;

        Ok(())
    }

    pub async fn save_api_key(
        pool: &SqlitePool,
        provider: &str,
        api_key: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        Self::save_api_key_with_store(pool, default_store(), provider, api_key).await
    }

    /// Save an API key into the OS keychain, falling back to the legacy SQLite
    /// column when no keychain is available (e.g. headless Linux).
    pub async fn save_api_key_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        provider: &str,
        api_key: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        // Custom OpenAI uses JSON config (customOpenAIConfig) instead of a separate API key column
        if provider == "custom-openai" {
            return Err(sqlx::Error::Protocol(
                "custom-openai provider should use save_custom_openai_config() instead of save_api_key()".into(),
            ));
        }

        let Some(api_key_column) = api_key_column(provider)? else {
            return Ok(()); // Provider needs no API key
        };

        match store.set(provider, api_key) {
            Ok(()) => {
                // Key is in the keychain now; drop the plaintext copy.
                clear_legacy_column(pool, "settings", api_key_column).await?;
                Ok(())
            }
            Err(e) => {
                log::warn!(
                    "Keychain unavailable for provider '{}' ({}); storing API key in the database instead",
                    provider,
                    e
                );
                write_legacy_settings_column(pool, api_key_column, api_key).await
            }
        }
    }

    pub async fn get_api_key(
        pool: &SqlitePool,
        provider: &str,
    ) -> std::result::Result<Option<String>, sqlx::Error> {
        Self::get_api_key_with_store(pool, default_store(), provider).await
    }

    /// Read an API key: keychain first, then the legacy SQLite column (which is
    /// migrated into the keychain on a best-effort basis when found).
    pub async fn get_api_key_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        provider: &str,
    ) -> std::result::Result<Option<String>, sqlx::Error> {
        // Custom OpenAI uses JSON config - extract API key from there
        if provider == "custom-openai" {
            let config = Self::get_custom_openai_config_with_store(pool, store).await?;
            return Ok(config.and_then(|c| c.api_key));
        }

        let Some(api_key_column) = api_key_column(provider)? else {
            return Ok(None); // Provider needs no API key
        };

        match store.get(provider) {
            Ok(Some(key)) => return Ok(Some(key)),
            Ok(None) => {}
            Err(e) => log::warn!(
                "Keychain unavailable for provider '{}' ({}); reading API key from the database",
                provider,
                e
            ),
        }

        let legacy_key = read_legacy_column(pool, "settings", api_key_column).await?;

        if let Some(key) = legacy_key.as_deref() {
            // Best-effort migration of a plaintext key into the keychain.
            match store.set(provider, key) {
                Ok(()) => {
                    clear_legacy_column(pool, "settings", api_key_column).await?;
                    log::info!("Migrated API key for provider '{}' into the keychain", provider);
                }
                Err(e) => log::warn!(
                    "Could not migrate API key for provider '{}' into the keychain: {}",
                    provider,
                    e
                ),
            }
        }

        Ok(legacy_key)
    }

    pub async fn get_transcript_config(
        pool: &SqlitePool,
    ) -> std::result::Result<Option<TranscriptSetting>, sqlx::Error> {
        let setting =
            sqlx::query_as::<_, TranscriptSetting>("SELECT * FROM transcript_settings LIMIT 1")
                .fetch_optional(pool)
                .await?;
        Ok(setting)

    }

    pub async fn save_transcript_config(
        pool: &SqlitePool,
        provider: &str,
        model: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        sqlx::query(
            r#"
            INSERT INTO transcript_settings (id, provider, model)
            VALUES ('1', $1, $2)
            ON CONFLICT(id) DO UPDATE SET
                provider = excluded.provider,
                model = excluded.model
            "#,
        )
        .bind(provider)
        .bind(model)
        .execute(pool)
        .await?;

        Ok(())
    }

    pub async fn save_transcript_api_key(
        pool: &SqlitePool,
        provider: &str,
        api_key: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        Self::save_transcript_api_key_with_store(pool, default_store(), provider, api_key).await
    }

    /// Save a transcript provider's API key into the OS keychain, falling back
    /// to the legacy SQLite column when no keychain is available.
    pub async fn save_transcript_api_key_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        provider: &str,
        api_key: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        let Some(api_key_column) = transcript_api_key_column(provider)? else {
            return Ok(()); // Provider needs no API key
        };

        match store.set(&transcript_secret_account(provider), api_key) {
            Ok(()) => {
                // Key is in the keychain now; drop the plaintext copy.
                clear_legacy_column(pool, "transcript_settings", api_key_column).await?;
                Ok(())
            }
            Err(e) => {
                log::warn!(
                    "Keychain unavailable for transcript provider '{}' ({}); storing API key in the database instead",
                    provider,
                    e
                );
                write_legacy_transcript_column(pool, api_key_column, api_key).await
            }
        }
    }

    pub async fn get_transcript_api_key(
        pool: &SqlitePool,
        provider: &str,
    ) -> std::result::Result<Option<String>, sqlx::Error> {
        Self::get_transcript_api_key_with_store(pool, default_store(), provider).await
    }

    /// Read a transcript provider's API key: keychain first, then the legacy
    /// SQLite column (migrated into the keychain best-effort when found).
    pub async fn get_transcript_api_key_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        provider: &str,
    ) -> std::result::Result<Option<String>, sqlx::Error> {
        let Some(api_key_column) = transcript_api_key_column(provider)? else {
            return Ok(None); // Provider needs no API key
        };

        let account = transcript_secret_account(provider);

        match store.get(&account) {
            Ok(Some(key)) => return Ok(Some(key)),
            Ok(None) => {}
            Err(e) => log::warn!(
                "Keychain unavailable for transcript provider '{}' ({}); reading API key from the database",
                provider,
                e
            ),
        }

        let legacy_key = read_legacy_column(pool, "transcript_settings", api_key_column).await?;

        if let Some(key) = legacy_key.as_deref() {
            // Best-effort migration of a plaintext key into the keychain.
            match store.set(&account, key) {
                Ok(()) => {
                    clear_legacy_column(pool, "transcript_settings", api_key_column).await?;
                    log::info!(
                        "Migrated API key for transcript provider '{}' into the keychain",
                        provider
                    );
                }
                Err(e) => log::warn!(
                    "Could not migrate API key for transcript provider '{}' into the keychain: {}",
                    provider,
                    e
                ),
            }
        }

        Ok(legacy_key)
    }

    pub async fn delete_api_key(
        pool: &SqlitePool,
        provider: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        Self::delete_api_key_with_store(pool, default_store(), provider).await
    }

    /// Delete an API key from both the keychain and the legacy SQLite column.
    pub async fn delete_api_key_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        provider: &str,
    ) -> std::result::Result<(), sqlx::Error> {
        if let Err(e) = store.delete(provider) {
            log::warn!(
                "Failed to delete keychain entry for provider '{}': {}",
                provider,
                e
            );
        }

        // Custom OpenAI uses JSON config - clear the entire config
        if provider == "custom-openai" {
            sqlx::query("UPDATE settings SET customOpenAIConfig = NULL WHERE id = '1'")
                .execute(pool)
                .await?;
            return Ok(());
        }

        let Some(api_key_column) = api_key_column(provider)? else {
            return Ok(()); // Provider needs no API key
        };

        clear_legacy_column(pool, "settings", api_key_column).await
    }

    // ===== CUSTOM OPENAI CONFIG METHODS =====

    /// Gets the custom OpenAI configuration from JSON
    ///
    /// # Returns
    /// * `Ok(Some(CustomOpenAIConfig))` - Config exists and is valid JSON
    /// * `Ok(None)` - No config stored
    /// * `Err(sqlx::Error)` - Database error
    pub async fn get_custom_openai_config(
        pool: &SqlitePool,
    ) -> std::result::Result<Option<CustomOpenAIConfig>, sqlx::Error> {
        Self::get_custom_openai_config_with_store(pool, default_store()).await
    }

    /// Same as [`Self::get_custom_openai_config`], with an injectable secret store.
    ///
    /// The stored JSON never contains the API key: it is read from the keychain
    /// under the provider id "custom-openai". A legacy key still embedded in the
    /// JSON is migrated into the keychain (best effort) and stripped from the JSON.
    pub async fn get_custom_openai_config_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
    ) -> std::result::Result<Option<CustomOpenAIConfig>, sqlx::Error> {
        use sqlx::Row;

        let row = sqlx::query(
            r#"
            SELECT customOpenAIConfig
            FROM settings
            WHERE id = '1'
            LIMIT 1
            "#
        )
        .fetch_optional(pool)
        .await?;

        let Some(record) = row else {
            return Ok(None);
        };

        let config_json: Option<String> = record.get("customOpenAIConfig");
        let Some(json) = config_json else {
            return Ok(None);
        };

        // Parse JSON into CustomOpenAIConfig
        let mut config: CustomOpenAIConfig = serde_json::from_str(&json)
            .map_err(|e| sqlx::Error::Protocol(
                format!("Invalid JSON in customOpenAIConfig: {}", e).into()
            ))?;

        match config.api_key.take() {
            // Legacy config with the key inside the JSON: migrate it out.
            Some(legacy_key) if !legacy_key.trim().is_empty() => {
                match store.set(CUSTOM_OPENAI_PROVIDER, &legacy_key) {
                    Ok(()) => {
                        Self::write_custom_openai_json(pool, &config).await?;
                        log::info!("Migrated custom-openai API key into the keychain");
                    }
                    Err(e) => log::warn!(
                        "Could not migrate custom-openai API key into the keychain: {}",
                        e
                    ),
                }
                config.api_key = Some(legacy_key);
            }
            _ => {
                config.api_key = match store.get(CUSTOM_OPENAI_PROVIDER) {
                    Ok(key) => key,
                    Err(e) => {
                        log::warn!(
                            "Keychain unavailable for custom-openai ({}); no API key available",
                            e
                        );
                        None
                    }
                };
            }
        }

        Ok(Some(config))
    }

    /// Saves the custom OpenAI configuration as JSON
    ///
    /// The API key is stored in the OS keychain rather than the JSON blob; on a
    /// keychain failure it falls back to the JSON so the config still works.
    ///
    /// # Arguments
    /// * `pool` - Database connection pool
    /// * `config` - CustomOpenAIConfig to save (includes endpoint, apiKey, model, maxTokens, temperature, topP)
    ///
    /// # Returns
    /// * `Ok(())` - Config saved successfully
    /// * `Err(sqlx::Error)` - Database or JSON serialization error
    pub async fn save_custom_openai_config(
        pool: &SqlitePool,
        config: &CustomOpenAIConfig,
    ) -> std::result::Result<(), sqlx::Error> {
        Self::save_custom_openai_config_with_store(pool, default_store(), config).await
    }

    /// Same as [`Self::save_custom_openai_config`], with an injectable secret store.
    pub async fn save_custom_openai_config_with_store(
        pool: &SqlitePool,
        store: &dyn SecretStore,
        config: &CustomOpenAIConfig,
    ) -> std::result::Result<(), sqlx::Error> {
        let mut stored = config.clone();

        match config.api_key.as_deref() {
            Some(key) if !key.trim().is_empty() => match store.set(CUSTOM_OPENAI_PROVIDER, key) {
                Ok(()) => stored.api_key = None,
                Err(e) => log::warn!(
                    "Keychain unavailable for custom-openai ({}); storing API key in the database instead",
                    e
                ),
            },
            _ => {
                stored.api_key = None;
                if let Err(e) = store.delete(CUSTOM_OPENAI_PROVIDER) {
                    log::warn!("Failed to clear custom-openai keychain entry: {}", e);
                }
            }
        }

        Self::write_custom_openai_json(pool, &stored).await
    }

    /// Serialize a config into the customOpenAIConfig column verbatim.
    async fn write_custom_openai_json(
        pool: &SqlitePool,
        config: &CustomOpenAIConfig,
    ) -> std::result::Result<(), sqlx::Error> {
        let config_json = serde_json::to_string(config)
            .map_err(|e| sqlx::Error::Protocol(
                format!("Failed to serialize config to JSON: {}", e).into()
            ))?;

        // Upsert into settings table
        sqlx::query(
            r#"
            INSERT INTO settings (id, provider, model, whisperModel, customOpenAIConfig)
            VALUES ('1', 'custom-openai', $1, 'large-v3', $2)
            ON CONFLICT(id) DO UPDATE SET
                customOpenAIConfig = excluded.customOpenAIConfig
            "#,
        )
        .bind(&config.model)
        .bind(config_json)
        .execute(pool)
        .await?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::database::manager::DatabaseManager;
    use crate::database::secrets::test_support::MockSecretStore;

    async fn temp_pool() -> (tempfile::TempDir, DatabaseManager) {
        let dir = tempfile::tempdir().expect("tempdir");
        let db_path = dir
            .path()
            .join("meeting_minutes.sqlite")
            .to_string_lossy()
            .to_string();
        let legacy_path = dir
            .path()
            .join("does_not_exist.db")
            .to_string_lossy()
            .to_string();
        let manager = DatabaseManager::new(&db_path, &legacy_path)
            .await
            .expect("open database");
        (dir, manager)
    }

    async fn legacy_value(pool: &SqlitePool, column: &str) -> Option<String> {
        read_legacy_column(pool, "settings", column)
            .await
            .expect("read column")
    }

    #[tokio::test]
    async fn save_api_key_prefers_the_keychain_and_clears_the_column() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        // Pre-existing plaintext key, as written by an older build.
        write_legacy_settings_column(pool, "openaiApiKey", "sk-old")
            .await
            .unwrap();

        SettingsRepository::save_api_key_with_store(pool, &store, "openai", "sk-new")
            .await
            .unwrap();

        assert_eq!(store.peek("openai").as_deref(), Some("sk-new"));
        assert_eq!(legacy_value(pool, "openaiApiKey").await, None);

        let read_back = SettingsRepository::get_api_key_with_store(pool, &store, "openai")
            .await
            .unwrap();
        assert_eq!(read_back.as_deref(), Some("sk-new"));
    }

    #[tokio::test]
    async fn save_api_key_falls_back_to_sqlite_without_a_keychain() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::unavailable();

        SettingsRepository::save_api_key_with_store(pool, &store, "claude", "sk-fallback")
            .await
            .unwrap();

        assert_eq!(
            legacy_value(pool, "anthropicApiKey").await.as_deref(),
            Some("sk-fallback")
        );

        // Reading still works when the keychain stays unavailable.
        let read_back = SettingsRepository::get_api_key_with_store(pool, &store, "claude")
            .await
            .unwrap();
        assert_eq!(read_back.as_deref(), Some("sk-fallback"));
    }

    #[tokio::test]
    async fn get_api_key_migrates_a_legacy_column_into_the_keychain() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        write_legacy_settings_column(pool, "groqApiKey", "gsk-legacy")
            .await
            .unwrap();

        let key = SettingsRepository::get_api_key_with_store(pool, &store, "groq")
            .await
            .unwrap();

        assert_eq!(key.as_deref(), Some("gsk-legacy"));
        assert_eq!(store.peek("groq").as_deref(), Some("gsk-legacy"));
        assert_eq!(legacy_value(pool, "groqApiKey").await, None);
    }

    #[tokio::test]
    async fn get_api_key_returns_none_when_nothing_is_stored() {
        let (_dir, db) = temp_pool().await;
        let store = MockSecretStore::new();

        let key = SettingsRepository::get_api_key_with_store(db.pool(), &store, "openrouter")
            .await
            .unwrap();
        assert_eq!(key, None);
    }

    #[tokio::test]
    async fn delete_api_key_clears_both_stores() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        write_legacy_settings_column(pool, "openaiApiKey", "sk-old")
            .await
            .unwrap();
        store.set("openai", "sk-new").unwrap();

        SettingsRepository::delete_api_key_with_store(pool, &store, "openai")
            .await
            .unwrap();

        assert_eq!(store.peek("openai"), None);
        assert_eq!(legacy_value(pool, "openaiApiKey").await, None);
    }

    async fn transcript_legacy_value(pool: &SqlitePool, column: &str) -> Option<String> {
        read_legacy_column(pool, "transcript_settings", column)
            .await
            .expect("read column")
    }

    #[tokio::test]
    async fn get_api_key_treats_a_blank_legacy_column_as_absent() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        write_legacy_settings_column(pool, "openaiApiKey", "   ")
            .await
            .unwrap();

        let key = SettingsRepository::get_api_key_with_store(pool, &store, "openai")
            .await
            .unwrap();
        assert_eq!(key, None, "a blank column value is not a key");
        assert_eq!(store.peek("openai"), None, "nothing to migrate");
    }

    #[tokio::test]
    async fn save_transcript_api_key_prefers_the_keychain_and_clears_the_column() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        write_legacy_transcript_column(pool, "deepgramApiKey", "dg-old")
            .await
            .unwrap();

        SettingsRepository::save_transcript_api_key_with_store(pool, &store, "deepgram", "dg-new")
            .await
            .unwrap();

        // Namespaced so it cannot collide with the summary provider of the same name.
        assert_eq!(store.peek("transcript:deepgram").as_deref(), Some("dg-new"));
        assert_eq!(transcript_legacy_value(pool, "deepgramApiKey").await, None);

        let read_back =
            SettingsRepository::get_transcript_api_key_with_store(pool, &store, "deepgram")
                .await
                .unwrap();
        assert_eq!(read_back.as_deref(), Some("dg-new"));
    }

    #[tokio::test]
    async fn save_transcript_api_key_falls_back_to_sqlite_without_a_keychain() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::unavailable();

        SettingsRepository::save_transcript_api_key_with_store(
            pool,
            &store,
            "elevenLabs",
            "el-fallback",
        )
        .await
        .unwrap();

        assert_eq!(
            transcript_legacy_value(pool, "elevenLabsApiKey")
                .await
                .as_deref(),
            Some("el-fallback")
        );

        let read_back =
            SettingsRepository::get_transcript_api_key_with_store(pool, &store, "elevenLabs")
                .await
                .unwrap();
        assert_eq!(read_back.as_deref(), Some("el-fallback"));
    }

    #[tokio::test]
    async fn get_transcript_api_key_migrates_a_legacy_column_into_the_keychain() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        write_legacy_transcript_column(pool, "groqApiKey", "gsk-transcript-legacy")
            .await
            .unwrap();

        let key = SettingsRepository::get_transcript_api_key_with_store(pool, &store, "groq")
            .await
            .unwrap();

        assert_eq!(key.as_deref(), Some("gsk-transcript-legacy"));
        assert_eq!(
            store.peek("transcript:groq").as_deref(),
            Some("gsk-transcript-legacy")
        );
        assert_eq!(transcript_legacy_value(pool, "groqApiKey").await, None);
        // The summary-side groq key is a separate account and untouched.
        assert_eq!(store.peek("groq"), None);
    }

    #[tokio::test]
    async fn transcript_providers_without_keys_are_no_ops() {
        let (_dir, db) = temp_pool().await;
        let store = MockSecretStore::new();

        SettingsRepository::save_transcript_api_key_with_store(db.pool(), &store, "parakeet", "x")
            .await
            .unwrap();
        assert_eq!(store.peek("transcript:parakeet"), None);

        let key =
            SettingsRepository::get_transcript_api_key_with_store(db.pool(), &store, "parakeet")
                .await
                .unwrap();
        assert_eq!(key, None);
    }

    #[tokio::test]
    async fn custom_openai_key_is_stored_outside_the_json_config() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        let config = CustomOpenAIConfig {
            endpoint: "http://localhost:8000/v1".to_string(),
            api_key: Some("sk-custom".to_string()),
            model: "llama-3-70b".to_string(),
            max_tokens: None,
            temperature: None,
            top_p: None,
        };

        SettingsRepository::save_custom_openai_config_with_store(pool, &store, &config)
            .await
            .unwrap();

        assert_eq!(store.peek("custom-openai").as_deref(), Some("sk-custom"));

        let raw: Option<String> =
            sqlx::query_scalar("SELECT customOpenAIConfig FROM settings WHERE id = '1'")
                .fetch_one(pool)
                .await
                .unwrap();
        let raw = raw.expect("config json");
        assert!(
            !raw.contains("sk-custom"),
            "API key must not be written into the JSON config: {raw}"
        );

        let loaded = SettingsRepository::get_custom_openai_config_with_store(pool, &store)
            .await
            .unwrap()
            .expect("config");
        assert_eq!(loaded.api_key.as_deref(), Some("sk-custom"));
        assert_eq!(loaded.endpoint, config.endpoint);

        // get_api_key routes custom-openai through the same store.
        let via_get = SettingsRepository::get_api_key_with_store(pool, &store, "custom-openai")
            .await
            .unwrap();
        assert_eq!(via_get.as_deref(), Some("sk-custom"));
    }

    #[tokio::test]
    async fn legacy_custom_openai_key_in_json_is_migrated() {
        let (_dir, db) = temp_pool().await;
        let pool = db.pool();
        let store = MockSecretStore::new();

        // Simulate an older build that wrote the key inside the JSON.
        let legacy_json = r#"{"endpoint":"http://localhost:8000/v1","apiKey":"sk-legacy","model":"m","maxTokens":null,"temperature":null,"topP":null}"#;
        sqlx::query(
            "INSERT INTO settings (id, provider, model, whisperModel, customOpenAIConfig)
             VALUES ('1', 'custom-openai', 'm', 'large-v3', $1)",
        )
        .bind(legacy_json)
        .execute(pool)
        .await
        .unwrap();

        let loaded = SettingsRepository::get_custom_openai_config_with_store(pool, &store)
            .await
            .unwrap()
            .expect("config");
        assert_eq!(loaded.api_key.as_deref(), Some("sk-legacy"));
        assert_eq!(store.peek("custom-openai").as_deref(), Some("sk-legacy"));

        let raw: Option<String> =
            sqlx::query_scalar("SELECT customOpenAIConfig FROM settings WHERE id = '1'")
                .fetch_one(pool)
                .await
                .unwrap();
        assert!(!raw.expect("json").contains("sk-legacy"));
    }
}
