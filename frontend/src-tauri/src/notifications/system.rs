use crate::notifications::types::{Notification, NotificationTimeout};
use anyhow::{Result, anyhow};
use log::{info as log_info, error as log_error};
use tauri::{AppHandle, Runtime};
use tauri_plugin_notification::NotificationExt;
use std::time::Duration;

/// Cross-platform system notification handler
pub struct SystemNotificationHandler<R: Runtime> {
    app_handle: AppHandle<R>,
}

impl<R: Runtime> SystemNotificationHandler<R> {
    pub fn new(app_handle: AppHandle<R>) -> Self {
        Self {
            app_handle,
        }
    }

    /// Show a notification using Tauri's notification plugin
    pub async fn show_notification(&self, notification: Notification) -> Result<()> {
        log_info!("Attempting to show notification: {}", notification.title);

        // DND policy lives in NotificationManager, which knows whether the user
        // asked us to respect system Do Not Disturb; deciding again here would
        // override that setting.

        // Use Tauri notification for all platforms
        log_info!("Showing Tauri notification: {}", notification.title);

        let builder = self.app_handle.notification().builder()
            .title(&notification.title)
            .body(&notification.body);

        match builder.show() {
            Ok(_) => {
                log_info!("Successfully showed Tauri notification: {}", notification.title);
                Ok(())
            }
            Err(e) => {
                log_error!("Failed to show Tauri notification: {}", e);
                Err(anyhow!("Failed to show notification: {}", e))
            }
        }
    }

    /// Check if system Do Not Disturb is currently active.
    ///
    /// An unknown status (unsupported platform, unreadable state) counts as
    /// "not active" so notifications are never silently swallowed.
    pub async fn is_dnd_active(&self) -> bool {
        self.get_system_dnd_status().await.unwrap_or(false)
    }

    /// Get the actual system DND status.
    ///
    /// Returns `None` when the platform cannot report it (Windows, Linux) or
    /// when the macOS Focus state could not be read.
    pub async fn get_system_dnd_status(&self) -> Option<bool> {
        read_system_dnd_status()
    }

    /// Request notification permission from the system
    pub async fn request_permission(&self) -> Result<bool> {
        log_info!("Requesting notification permission");

        // On most platforms with Tauri, permissions are handled automatically
        // We don't need to show a test notification during initialization
        log_info!("Notification permission granted (automatic for Tauri apps)");
        Ok(true)
    }

    /// Show a test notification to verify the system is working
    #[allow(dead_code)] // Used by show_test_notification command for manual testing
    async fn show_test_notification(&self) -> Result<()> {
        let test_notification = Notification::test_notification();
        self.show_notification(test_notification).await
    }

    /// Clear all notifications (platform-specific)
    pub async fn clear_notifications(&self) -> Result<()> {
        log_info!("Clearing all notifications");

        // This is platform-specific and complex to implement
        // For now, we'll just log that we attempted to clear
        // Future enhancement can add platform-specific clearing

        Ok(())
    }
}

/// Read the system Do Not Disturb / Focus status.
///
/// macOS stores active Focus assertions in
/// `~/Library/DoNotDisturb/DB/Assertions.json`; a non-empty
/// `data[0].storeAssertionRecords` array means a Focus mode is on.
/// Other platforms have no equivalent we can read, so they report `None`.
#[cfg(target_os = "macos")]
fn read_system_dnd_status() -> Option<bool> {
    let path = dirs::home_dir()?
        .join("Library")
        .join("DoNotDisturb")
        .join("DB")
        .join("Assertions.json");

    let contents = std::fs::read_to_string(&path).ok()?;
    let parsed: serde_json::Value = serde_json::from_str(&contents).ok()?;

    let records = parsed
        .get("data")?
        .get(0)?
        .get("storeAssertionRecords");

    match records {
        Some(serde_json::Value::Array(records)) => Some(!records.is_empty()),
        // The key is absent (or null) when no Focus mode is active.
        Some(serde_json::Value::Null) | None => Some(false),
        Some(_) => None,
    }
}

/// Windows and Linux expose no readable DND state, so the status is unknown.
#[cfg(not(target_os = "macos"))]
fn read_system_dnd_status() -> Option<bool> {
    None
}

/// Convert notification timeout to duration
impl From<&NotificationTimeout> for Option<Duration> {
    fn from(timeout: &NotificationTimeout) -> Self {
        match timeout {
            NotificationTimeout::Never => None,
            NotificationTimeout::Seconds(secs) => Some(Duration::from_secs(*secs)),
            NotificationTimeout::Default => Some(Duration::from_secs(5)),
        }
    }
}