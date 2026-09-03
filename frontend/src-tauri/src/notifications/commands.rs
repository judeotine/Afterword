use crate::notifications::{
    types::Notification,
    settings::NotificationSettings,
    manager::NotificationManager,
};

use anyhow::Result;
use log::{info as log_info, error as log_error, debug as log_debug};
use tauri::{State, AppHandle, Runtime, Wry};
use tauri_plugin_notification::NotificationExt;
use std::sync::Arc;
use tokio::sync::RwLock;

/// Shared notification manager state
pub type NotificationManagerState<R> = Arc<RwLock<Option<NotificationManager<R>>>>;

/// Initialize the notification manager (called during app setup)
pub async fn initialize_notification_manager<R: Runtime>(
    app_handle: AppHandle<R>,
) -> Result<NotificationManager<R>> {
    log_info!("Initializing notification manager...");

    let manager = NotificationManager::new(app_handle).await?;
    manager.initialize().await?;

    log_info!("Notification manager initialized successfully");
    Ok(manager)
}

/// Get notification settings
#[tauri::command]
pub async fn get_notification_settings(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<NotificationSettings, String> {
    log_info!("Getting notification settings");

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        Ok(manager.get_settings().await)
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Set notification settings
#[tauri::command]
pub async fn set_notification_settings(
    settings: NotificationSettings,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Setting notification settings");

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.update_settings(settings).await
            .map_err(|e| format!("Failed to update settings: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Request notification permission from the system
#[tauri::command]
pub async fn request_notification_permission(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<bool, String> {
    log_info!("Requesting notification permission");

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.request_permission().await
            .map_err(|e| format!("Failed to request permission: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Show a custom notification
#[tauri::command]
pub async fn show_notification(
    notification: Notification,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Showing custom notification: {}", notification.title);

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_notification(notification).await
            .map_err(|e| format!("Failed to show notification: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Show a test notification
#[tauri::command]
pub async fn show_test_notification(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Showing test notification");

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_test_notification().await
            .map_err(|e| format!("Failed to show test notification: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Check if Do Not Disturb is active
#[tauri::command]
pub async fn is_dnd_active(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<bool, String> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        Ok(manager.is_dnd_active().await)
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// System Do Not Disturb status as reported to the frontend.
///
/// `supported` is false on platforms where the state cannot be read (Windows,
/// Linux) or when macOS Focus state is unreadable; `active` is then always false.
#[derive(Debug, Clone, Copy, serde::Serialize)]
pub struct SystemDndStatus {
    pub supported: bool,
    pub active: bool,
}

/// Get system Do Not Disturb status
#[tauri::command]
pub async fn get_system_dnd_status(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<SystemDndStatus, String> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        Ok(match manager.get_system_dnd_status().await {
            Some(active) => SystemDndStatus { supported: true, active },
            None => SystemDndStatus { supported: false, active: false },
        })
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Set manual Do Not Disturb mode
#[tauri::command]
pub async fn set_manual_dnd(
    enabled: bool,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Setting manual DND mode: {}", enabled);

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.set_manual_dnd(enabled).await
            .map_err(|e| format!("Failed to set manual DND: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Set user consent for notifications
#[tauri::command]
pub async fn set_notification_consent(
    consent: bool,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Setting notification consent: {}", consent);

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.set_consent(consent).await
            .map_err(|e| format!("Failed to set consent: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Clear all notifications
#[tauri::command]
pub async fn clear_notifications(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Clearing all notifications");

    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.clear_notifications().await
            .map_err(|e| format!("Failed to clear notifications: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Check if notification system is ready
#[tauri::command]
pub async fn is_notification_system_ready(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<bool, String> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        Ok(manager.is_ready().await)
    } else {
        Ok(false)
    }
}

/// Initialize notification manager manually (for testing and ensuring it's ready)
#[tauri::command]
pub async fn initialize_notification_manager_manual(
    app: AppHandle<Wry>,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Manual initialization of notification manager requested");

    let manager_lock = manager_state.read().await;
    if manager_lock.is_some() {
        return Ok(()); // Already initialized
    }
    drop(manager_lock);

    // Initialize the manager
    match initialize_notification_manager(app).await {
        Ok(manager) => {
            let mut state = manager_state.write().await;
            *state = Some(manager);
            log_info!("Notification manager initialized successfully via manual command");
            Ok(())
        }
        Err(e) => {
            log_error!("Failed to initialize notification manager manually: {}", e);
            Err(format!("Failed to initialize notification manager: {}", e))
        }
    }
}

/// Test notification with automatic consent for development/testing
#[tauri::command]
pub async fn test_notification_with_auto_consent(
    app: AppHandle<Wry>,
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<(), String> {
    log_info!("Testing notification with automatic consent");

    // First ensure manager is initialized
    let manager_lock = manager_state.read().await;
    if manager_lock.is_none() {
        drop(manager_lock);
        if let Err(e) = initialize_notification_manager_manual(app.clone(), manager_state.clone()).await {
            return Err(format!("Failed to initialize manager: {}", e));
        }
    } else {
        drop(manager_lock);
    }

    // Get the manager again
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        // Set consent and permissions automatically for testing
        if let Err(e) = manager.set_consent(true).await {
            log_error!("Failed to set consent: {}", e);
        }
        if let Err(e) = manager.request_permission().await {
            log_error!("Failed to request permission: {}", e);
        }

        // Show test notification
        manager.show_test_notification().await
            .map_err(|e| format!("Failed to show test notification: {}", e))
    } else {
        Err("Manager still not initialized".to_string())
    }
}

/// Get notification system statistics
#[tauri::command]
pub async fn get_notification_stats(
    manager_state: State<'_, NotificationManagerState<Wry>>
) -> Result<serde_json::Value, String> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        let stats = manager.get_stats().await;
        serde_json::to_value(stats)
            .map_err(|e| format!("Failed to serialize stats: {}", e))
    } else {
        Err("Notification manager not initialized".to_string())
    }
}

/// Whether a fallback notification may be shown.
///
/// The fallback path runs when the notification manager could not be
/// initialized, so it has to reproduce the manager's consent checks itself:
/// notifications are opt-in, and neither user consent nor the system
/// permission may be assumed. `preference` is the per-notification-type
/// preference for the notification about to be shown.
fn fallback_allowed(settings: &NotificationSettings, preference: bool) -> bool {
    settings.consent_given && settings.system_permission_granted && preference
}

/// Load settings for the fallback path through the same migration-aware call
/// the manager uses, so a fallback can never see a more permissive view of
/// consent than the manager would.
async fn load_fallback_settings<R: Runtime>(
    app_handle: &tauri::AppHandle<R>,
) -> Option<NotificationSettings> {
    match crate::notifications::settings::ConsentManager::new(app_handle.clone()) {
        Ok(consent_manager) => match consent_manager.get_settings_with_migration().await {
            Ok(settings) => Some(settings),
            Err(e) => {
                log_debug!("Could not load notification settings for fallback: {}", e);
                None
            }
        },
        Err(e) => {
            log_debug!("Could not open notification settings for fallback: {}", e);
            None
        }
    }
}

// Helper functions for showing specific notification types
// These are used internally by the app and don't need to be Tauri commands

/// Show recording started notification (internal use)
pub async fn show_recording_started_notification<R: Runtime>(
    app_handle: &tauri::AppHandle<R>,
    manager_state: &NotificationManagerState<R>,
    meeting_name: Option<String>,
) -> Result<()> {
    log_info!("Attempting to show recording started notification for meeting: {:?}", meeting_name);

    // Check if manager is initialized
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        log_info!("Notification manager found, showing recording started notification");
        manager.show_recording_started(meeting_name).await
    } else {
        drop(manager_lock);
        log_info!("Notification manager not initialized, initializing now...");

        // Try to initialize the manager first
        match initialize_notification_manager(app_handle.clone()).await {
            Ok(manager) => {
                // Store the manager in the state
                let mut state_lock = manager_state.write().await;
                *state_lock = Some(manager);
                drop(state_lock);

                log_info!("Notification manager initialized, showing notification...");

                // Now use the initialized manager
                let manager_lock = manager_state.read().await;
                if let Some(manager) = manager_lock.as_ref() {
                    manager.show_recording_started(meeting_name).await
                } else {
                    log_error!("Manager still not available after initialization");
                    Ok(())
                }
            }
            Err(e) => {
                log_error!("Failed to initialize notification manager: {}", e);

                // Consent gate: the fallback must honour the same opt-in the
                // manager enforces, not just the per-type preference.
                let Some(settings) = load_fallback_settings(app_handle).await else {
                    log_debug!("Skipping fallback notification: notification settings unavailable");
                    return Ok(());
                };

                if !fallback_allowed(
                    &settings,
                    settings.notification_preferences.show_recording_started,
                ) {
                    log_debug!("Skipping fallback recording started notification: not consented or disabled");
                    return Ok(());
                }

                // Fallback: Use Tauri's notification API directly
                let title = "Afterword";
                let body = match meeting_name {
                    Some(name) => format!("Recording started for meeting: {}", name),
                    None => "Recording has started. Please inform others in the meeting that you are recording.".to_string(),
                };

                log_info!("Using direct Tauri notification fallback: {} - {}", title, body);

                match app_handle.notification().builder()
                    .title(title)
                    .body(body)
                    .show()
                {
                    Ok(_) => {
                        log_info!("Successfully showed fallback notification: {}", title);
                        Ok(())
                    }
                    Err(e) => {
                        log_error!("Failed to show fallback notification: {}", e);
                        Err(anyhow::anyhow!("Failed to show notification: {}", e))
                    }
                }
            }
        }
    }
}

/// Show recording stopped notification (internal use)
pub async fn show_recording_stopped_notification<R: Runtime>(
    app_handle: &tauri::AppHandle<R>,
    manager_state: &NotificationManagerState<R>,
) -> Result<()> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_recording_stopped().await
    } else {
        drop(manager_lock);
        log_info!("Notification manager not initialized for stop notification, using fallback...");

        // Consent gate: the fallback must honour the same opt-in the manager
        // enforces, not just the per-type preference.
        let Some(settings) = load_fallback_settings(app_handle).await else {
            log_debug!("Skipping fallback notification: notification settings unavailable");
            return Ok(());
        };

        if !fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_stopped,
        ) {
            log_debug!("Skipping fallback recording stopped notification: not consented or disabled");
            return Ok(());
        }

        // Use direct Tauri notification as fallback for stop notification
        let title = "Afterword";
        let body = "Recording has stopped";

        log_info!("Using direct Tauri notification fallback: {} - {}", title, body);

        match app_handle.notification().builder()
            .title(title)
            .body(body)
            .show()
        {
            Ok(_) => {
                log_info!("Successfully showed fallback notification: {}", title);
                Ok(())
            }
            Err(e) => {
                log_error!("Failed to show fallback notification: {}", e);
                Err(anyhow::anyhow!("Failed to show notification: {}", e))
            }
        }
    }
}

/// Show recording paused notification (internal use)
pub async fn show_recording_paused_notification(
    manager_state: &NotificationManagerState<Wry>,
) -> Result<()> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_recording_paused().await
    } else {
        log_error!("Cannot show recording paused notification: manager not initialized");
        Ok(())
    }
}

/// Show recording resumed notification (internal use)
pub async fn show_recording_resumed_notification(
    manager_state: &NotificationManagerState<Wry>,
) -> Result<()> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_recording_resumed().await
    } else {
        log_error!("Cannot show recording resumed notification: manager not initialized");
        Ok(())
    }
}

/// Show transcription complete notification (internal use)
pub async fn show_transcription_complete_notification(
    manager_state: &NotificationManagerState<Wry>,
    file_path: Option<String>,
) -> Result<()> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_transcription_complete(file_path).await
    } else {
        log_error!("Cannot show transcription complete notification: manager not initialized");
        Ok(())
    }
}

/// Show system error notification (internal use)
pub async fn show_system_error_notification(
    manager_state: &NotificationManagerState<Wry>,
    error: String,
) -> Result<()> {
    let manager_lock = manager_state.read().await;
    if let Some(manager) = manager_lock.as_ref() {
        manager.show_system_error(error).await
    } else {
        log_error!("Cannot show system error notification: manager not initialized");
        Ok(())
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::notifications::settings::NotificationSettings;

    fn consenting_settings() -> NotificationSettings {
        let mut settings = NotificationSettings::default();
        settings.consent_given = true;
        settings.system_permission_granted = true;
        settings.notification_preferences.show_recording_started = true;
        settings.notification_preferences.show_recording_stopped = true;
        settings
    }

    #[test]
    fn fallback_requires_consent() {
        let mut settings = consenting_settings();
        settings.consent_given = false;
        assert!(!fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_started
        ));
    }

    #[test]
    fn fallback_requires_system_permission() {
        let mut settings = consenting_settings();
        settings.system_permission_granted = false;
        assert!(!fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_started
        ));
    }

    #[test]
    fn fallback_requires_the_per_type_preference() {
        let mut settings = consenting_settings();
        settings.notification_preferences.show_recording_stopped = false;
        assert!(!fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_stopped
        ));
    }

    #[test]
    fn fallback_allowed_when_consented_and_enabled() {
        let settings = consenting_settings();
        assert!(fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_started
        ));
        assert!(fallback_allowed(
            &settings,
            settings.notification_preferences.show_recording_stopped
        ));
    }

    #[test]
    fn default_settings_never_allow_a_fallback() {
        let settings = NotificationSettings::default();
        assert!(!fallback_allowed(&settings, true));
    }
}
