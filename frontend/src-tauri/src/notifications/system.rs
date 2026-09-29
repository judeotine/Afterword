use crate::notifications::types::{Notification, NotificationTimeout};
use anyhow::{anyhow, Result};
use log::{error as log_error, info as log_info};
use std::time::Duration;
use tauri::{AppHandle, Runtime};
use tauri_plugin_notification::NotificationExt;

pub struct SystemNotificationHandler<R: Runtime> {
    app_handle: AppHandle<R>,
}

impl<R: Runtime> SystemNotificationHandler<R> {
    pub fn new(app_handle: AppHandle<R>) -> Self {
        Self { app_handle }
    }

    pub async fn show_notification(&self, notification: Notification) -> Result<()> {
        log_info!("Attempting to show notification: {}", notification.title);

        log_info!("Showing Tauri notification: {}", notification.title);

        let builder = self
            .app_handle
            .notification()
            .builder()
            .title(&notification.title)
            .body(&notification.body);

        match builder.show() {
            Ok(_) => {
                log_info!(
                    "Successfully showed Tauri notification: {}",
                    notification.title
                );
                Ok(())
            }
            Err(e) => {
                log_error!("Failed to show Tauri notification: {}", e);
                Err(anyhow!("Failed to show notification: {}", e))
            }
        }
    }

    pub async fn is_dnd_active(&self) -> bool {
        self.get_system_dnd_status().await.unwrap_or(false)
    }

    pub async fn get_system_dnd_status(&self) -> Option<bool> {
        read_system_dnd_status()
    }

    pub async fn request_permission(&self) -> Result<bool> {
        log_info!("Requesting notification permission");

        log_info!("Notification permission granted (automatic for Tauri apps)");
        Ok(true)
    }

    #[allow(dead_code)]
    async fn show_test_notification(&self) -> Result<()> {
        let test_notification = Notification::test_notification();
        self.show_notification(test_notification).await
    }

    pub async fn clear_notifications(&self) -> Result<()> {
        log_info!("Clearing all notifications");

        Ok(())
    }
}

#[cfg(target_os = "macos")]
fn read_system_dnd_status() -> Option<bool> {
    let path = dirs::home_dir()?
        .join("Library")
        .join("DoNotDisturb")
        .join("DB")
        .join("Assertions.json");

    let contents = std::fs::read_to_string(&path).ok()?;
    let parsed: serde_json::Value = serde_json::from_str(&contents).ok()?;

    let records = parsed.get("data")?.get(0)?.get("storeAssertionRecords");

    match records {
        Some(serde_json::Value::Array(records)) => Some(!records.is_empty()),

        Some(serde_json::Value::Null) | None => Some(false),
        Some(_) => None,
    }
}

#[cfg(not(target_os = "macos"))]
fn read_system_dnd_status() -> Option<bool> {
    None
}

impl From<&NotificationTimeout> for Option<Duration> {
    fn from(timeout: &NotificationTimeout) -> Self {
        match timeout {
            NotificationTimeout::Never => None,
            NotificationTimeout::Seconds(secs) => Some(Duration::from_secs(*secs)),
            NotificationTimeout::Default => Some(Duration::from_secs(5)),
        }
    }
}
