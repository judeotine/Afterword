use serde::{Deserialize, Serialize};
use tauri::State;

use crate::state::AppState;

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct SyncItem {
    pub id: String,
    pub meeting_id: String,
    pub kind: String,
    pub payload: String,
    pub status: String,
    pub attempts: i64,
    pub last_error: Option<String>,
}

#[tauri::command]
pub async fn sync_enqueue(
    state: State<'_, AppState>,
    id: String,
    meeting_id: String,
    kind: String,
    payload: String,
) -> Result<(), String> {
    sqlx::query(
        "INSERT INTO sync_queue (id, meeting_id, kind, payload) VALUES (?1, ?2, ?3, ?4)
         ON CONFLICT(id) DO UPDATE SET payload = excluded.payload, status = 'pending', updated_at = datetime('now')",
    )
    .bind(&id)
    .bind(&meeting_id)
    .bind(&kind)
    .bind(&payload)
    .execute(state.db_manager.pool())
    .await
    .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn sync_pending(state: State<'_, AppState>, limit: i64) -> Result<Vec<SyncItem>, String> {
    let capped = if limit <= 0 || limit > 200 { 50 } else { limit };
    let items = sqlx::query_as::<_, SyncItem>(
        "SELECT id, meeting_id, kind, payload, status, attempts, last_error
         FROM sync_queue
         WHERE status IN ('pending', 'retry') AND next_attempt_at <= datetime('now')
         ORDER BY next_attempt_at
         LIMIT ?1",
    )
    .bind(capped)
    .fetch_all(state.db_manager.pool())
    .await
    .map_err(|e| e.to_string())?;
    Ok(items)
}

#[tauri::command]
pub async fn sync_mark_done(state: State<'_, AppState>, id: String) -> Result<(), String> {
    sqlx::query("UPDATE sync_queue SET status = 'done', updated_at = datetime('now') WHERE id = ?1")
        .bind(&id)
        .execute(state.db_manager.pool())
        .await
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn sync_mark_failed(
    state: State<'_, AppState>,
    id: String,
    error: String,
    backoff_seconds: i64,
) -> Result<(), String> {
    let delay = if backoff_seconds < 0 { 0 } else { backoff_seconds };
    let next = format!("+{} seconds", delay);
    sqlx::query(
        "UPDATE sync_queue
         SET status = CASE WHEN attempts + 1 >= 5 THEN 'failed' ELSE 'retry' END,
             attempts = attempts + 1,
             last_error = ?2,
             next_attempt_at = datetime('now', ?3),
             updated_at = datetime('now')
         WHERE id = ?1",
    )
    .bind(&id)
    .bind(&error)
    .bind(&next)
    .execute(state.db_manager.pool())
    .await
    .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn sync_status_counts(state: State<'_, AppState>) -> Result<Vec<(String, i64)>, String> {
    let rows: Vec<(String, i64)> =
        sqlx::query_as("SELECT status, COUNT(*) FROM sync_queue GROUP BY status")
            .fetch_all(state.db_manager.pool())
            .await
            .map_err(|e| e.to_string())?;
    Ok(rows)
}
