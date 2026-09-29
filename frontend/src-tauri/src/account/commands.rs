use crate::database::secrets::default_store;

const ACCOUNT_REFRESH_KEY: &str = "account_refresh_token";

#[tauri::command]
pub async fn account_store_refresh_token(token: String) -> Result<(), String> {
    default_store()
        .set(ACCOUNT_REFRESH_KEY, &token)
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn account_get_refresh_token() -> Result<Option<String>, String> {
    default_store()
        .get(ACCOUNT_REFRESH_KEY)
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn account_clear_refresh_token() -> Result<(), String> {
    default_store()
        .delete(ACCOUNT_REFRESH_KEY)
        .map_err(|e| e.to_string())
}
