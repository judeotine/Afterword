use crate::notifications::{
    settings::{ConsentManager, NotificationSettings},
    system::SystemNotificationHandler,
    types::{Notification, NotificationType},
};
use anyhow::Result;
use log::{error as log_error, info as log_info, warn as log_warn};
use std::sync::Arc;
use tauri::{AppHandle, Runtime};
use tokio::sync::RwLock;

pub struct NotificationManager<R: Runtime> {
    #[allow(dead_code)]
    app_handle: AppHandle<R>,
    system_handler: Arc<SystemNotificationHandler<R>>,
    consent_manager: Arc<ConsentManager<R>>,
    settings: Arc<RwLock<NotificationSettings>>,
    initialized: Arc<RwLock<bool>>,
}

impl<R: Runtime> NotificationManager<R> {
    pub async fn new(app_handle: AppHandle<R>) -> Result<Self> {
        let system_handler = Arc::new(SystemNotificationHandler::new(app_handle.clone()));
        let consent_manager = Arc::new(ConsentManager::new(app_handle.clone())?);

        let settings = consent_manager
            .get_settings_with_migration()
            .await
            .unwrap_or_else(|_| NotificationSettings::default());

        let manager = Self {
            app_handle,
            system_handler,
            consent_manager,
            settings: Arc::new(RwLock::new(settings)),
            initialized: Arc::new(RwLock::new(false)),
        };

        log_info!("NotificationManager created successfully");
        Ok(manager)
    }

    pub async fn initialize(&self) -> Result<()> {
        let mut initialized = self.initialized.write().await;
        if *initialized {
            return Ok(());
        }

        log_info!("Initializing notification system...");

        if !self.consent_manager.has_consent().await {
            log_info!("First launch detected, notification consent will be requested by UI");
        }

        if self.consent_manager.has_consent().await
            && !self.consent_manager.has_system_permission().await
        {
            match self.request_permission().await {
                Ok(granted) => {
                    if granted {
                        log_info!("System notification permission granted");
                    } else {
                        log_warn!("System notification permission was not granted");
                    }
                }
                Err(e) => {
                    log_error!("Failed to request notification permission: {}", e);
                }
            }
        }

        *initialized = true;
        log_info!("Notification system initialized successfully");
        Ok(())
    }

    pub async fn show_notification(&self, notification: Notification) -> Result<()> {
        if !*self.initialized.read().await {
            self.initialize().await?;
        }

        if !self.should_show_notification(&notification).await {
            log_info!(
                "Skipping notification due to settings: {}",
                notification.title
            );
            return Ok(());
        }

        log_info!(
            "Showing notification: {} - {}",
            notification.title,
            notification.body
        );

        self.system_handler.show_notification(notification).await
    }

    pub async fn show_recording_started(&self, meeting_name: Option<String>) -> Result<()> {
        let settings = self.settings.read().await;
        log_info!(
            "🔔 Checking notification settings - show_recording_started: {}",
            settings.notification_preferences.show_recording_started
        );

        if !settings.notification_preferences.show_recording_started {
            log_info!("🚫 Recording started notification is disabled, skipping");
            return Ok(());
        }

        log_info!("✅ Recording started notification is enabled, showing notification");
        let notification = Notification::recording_started(meeting_name);
        self.show_notification(notification).await
    }

    pub async fn show_recording_stopped(&self) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings.notification_preferences.show_recording_stopped {
            return Ok(());
        }

        let notification = Notification::recording_stopped();
        self.show_notification(notification).await
    }

    pub async fn show_recording_paused(&self) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings.notification_preferences.show_recording_paused {
            return Ok(());
        }

        let notification = Notification::recording_paused();
        self.show_notification(notification).await
    }

    pub async fn show_recording_resumed(&self) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings.notification_preferences.show_recording_resumed {
            return Ok(());
        }

        let notification = Notification::recording_resumed();
        self.show_notification(notification).await
    }

    pub async fn show_transcription_complete(&self, file_path: Option<String>) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings
            .notification_preferences
            .show_transcription_complete
        {
            return Ok(());
        }

        let notification = Notification::transcription_complete(file_path);
        self.show_notification(notification).await
    }

    pub async fn show_meeting_reminder(
        &self,
        minutes_until: u64,
        meeting_title: Option<String>,
    ) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings.notification_preferences.show_meeting_reminders {
            return Ok(());
        }

        if !settings
            .notification_preferences
            .meeting_reminder_minutes
            .contains(&minutes_until)
        {
            return Ok(());
        }

        let notification = Notification::meeting_reminder(minutes_until, meeting_title);
        self.show_notification(notification).await
    }

    pub async fn show_system_error(&self, error: String) -> Result<()> {
        let settings = self.settings.read().await;
        if !settings.notification_preferences.show_system_errors {
            return Ok(());
        }

        let notification = Notification::system_error(error);
        self.show_notification(notification).await
    }

    pub async fn show_test_notification(&self) -> Result<()> {
        let notification = Notification::test_notification();
        self.system_handler.show_notification(notification).await
    }

    pub async fn get_settings(&self) -> NotificationSettings {
        self.settings.read().await.clone()
    }

    pub async fn update_settings(&self, new_settings: NotificationSettings) -> Result<()> {
        log_info!("📝 Updating notification settings:");
        log_info!(
            "   show_recording_started: {}",
            new_settings.notification_preferences.show_recording_started
        );
        log_info!(
            "   show_recording_stopped: {}",
            new_settings.notification_preferences.show_recording_stopped
        );

        let merged = {
            let current = self.settings.read().await;
            crate::notifications::settings::preserve_consent_state(&current, new_settings)
        };

        crate::notifications::settings::validate_settings(&merged)?;

        self.consent_manager.save_settings(&merged).await?;
        log_info!("💾 Settings saved to disk");

        let mut settings = self.settings.write().await;
        *settings = merged;

        log_info!("✅ Notification settings updated successfully");
        Ok(())
    }

    pub async fn is_dnd_active(&self) -> bool {
        let settings = self.settings.read().await;

        if settings.manual_dnd_mode {
            return true;
        }

        if settings.respect_do_not_disturb {
            self.system_handler.is_dnd_active().await
        } else {
            false
        }
    }

    pub async fn get_system_dnd_status(&self) -> Option<bool> {
        self.system_handler.get_system_dnd_status().await
    }

    pub async fn set_manual_dnd(&self, enabled: bool) -> Result<()> {
        self.consent_manager.set_dnd_mode(enabled).await?;

        let mut settings = self.settings.write().await;
        settings.manual_dnd_mode = enabled;

        log_info!("Manual DND mode set to: {}", enabled);
        Ok(())
    }

    pub async fn request_permission(&self) -> Result<bool> {
        let granted = self.system_handler.request_permission().await?;
        self.consent_manager.set_system_permission(granted).await?;

        let mut settings = self.settings.write().await;
        settings.system_permission_granted = granted;

        Ok(granted)
    }

    pub async fn set_consent(&self, consent: bool) -> Result<()> {
        self.consent_manager.set_consent(consent).await?;

        {
            let mut settings = self.settings.write().await;
            settings.consent_given = consent;
        }

        if consent {
            if let Err(e) = self.request_permission().await {
                log_warn!(
                    "Failed to request notification permission after consent: {}",
                    e
                );
            }
        }

        log_info!("User consent set to: {}", consent);
        Ok(())
    }

    async fn should_show_notification(&self, notification: &Notification) -> bool {
        let settings = self.settings.read().await;

        if !settings.consent_given || !settings.system_permission_granted {
            return false;
        }

        if self.is_dnd_active().await {
            match notification.priority {
                crate::notifications::types::NotificationPriority::Critical => {}
                _ => return false,
            }
        }

        match &notification.notification_type {
            NotificationType::RecordingStarted => {
                settings.notification_preferences.show_recording_started
            }
            NotificationType::RecordingStopped => {
                settings.notification_preferences.show_recording_stopped
            }
            NotificationType::RecordingPaused => {
                settings.notification_preferences.show_recording_paused
            }
            NotificationType::RecordingResumed => {
                settings.notification_preferences.show_recording_resumed
            }
            NotificationType::TranscriptionComplete => {
                settings
                    .notification_preferences
                    .show_transcription_complete
            }
            NotificationType::MeetingReminder(_) => {
                settings.notification_preferences.show_meeting_reminders
            }
            NotificationType::SystemError(_) => {
                settings.notification_preferences.show_system_errors
            }
            NotificationType::Test => true,
        }
    }

    pub async fn clear_notifications(&self) -> Result<()> {
        self.system_handler.clear_notifications().await
    }

    pub async fn is_ready(&self) -> bool {
        *self.initialized.read().await
    }

    pub async fn get_stats(&self) -> NotificationStats {
        let settings = self.settings.read().await;

        NotificationStats {
            consent_given: settings.consent_given,
            system_permission_granted: settings.system_permission_granted,
            manual_dnd_active: settings.manual_dnd_mode,
            system_dnd_active: self.get_system_dnd_status().await.unwrap_or(false),
            recording_notifications_enabled: settings
                .notification_preferences
                .show_recording_started,
            meeting_reminders_enabled: settings.notification_preferences.show_meeting_reminders,
        }
    }
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct NotificationStats {
    pub consent_given: bool,
    pub system_permission_granted: bool,
    pub manual_dnd_active: bool,
    pub system_dnd_active: bool,
    pub recording_notifications_enabled: bool,
    pub meeting_reminders_enabled: bool,
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::notifications::settings::NotificationSettings;
    use std::sync::Mutex as StdMutex;

    static SETTINGS_ENV_LOCK: StdMutex<()> = StdMutex::new(());

    struct TestApp {
        _dir: tempfile::TempDir,
        _app: tauri::App<tauri::test::MockRuntime>,
        manager: NotificationManager<tauri::test::MockRuntime>,
    }

    async fn test_manager(existing: Option<&str>) -> TestApp {
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("notifications.json");
        if let Some(contents) = existing {
            std::fs::write(&path, contents).expect("seed settings");
        }
        std::env::set_var("AFTERWORD_TEST_NOTIFICATION_SETTINGS", &path);

        let app = tauri::test::mock_app();
        let manager = NotificationManager::new(app.handle().clone())
            .await
            .expect("notification manager");

        TestApp {
            _dir: dir,
            _app: app,
            manager,
        }
    }

    #[tokio::test]
    async fn update_settings_cannot_revoke_consent() {
        let _guard = SETTINGS_ENV_LOCK.lock().unwrap();
        let app = test_manager(None).await;
        let manager = &app.manager;

        manager.set_consent(true).await.unwrap();
        assert!(manager.get_settings().await.consent_given);

        let mut stale = manager.get_settings().await;
        stale.consent_given = false;
        stale.system_permission_granted = false;
        stale.consent_migration_v1 = false;
        stale.respect_do_not_disturb = false;

        manager.update_settings(stale).await.unwrap();

        let settings = manager.get_settings().await;
        assert!(
            settings.consent_given,
            "consent must survive a settings write"
        );
        assert!(
            settings.system_permission_granted,
            "system permission must survive a settings write"
        );
        assert!(
            settings.consent_migration_v1,
            "migration marker must survive a settings write"
        );

        assert!(!settings.respect_do_not_disturb);
    }

    #[tokio::test]
    async fn set_consent_false_still_revokes_consent() {
        let _guard = SETTINGS_ENV_LOCK.lock().unwrap();
        let app = test_manager(None).await;
        let manager = &app.manager;

        manager.set_consent(true).await.unwrap();
        manager.set_consent(false).await.unwrap();

        assert!(!manager.get_settings().await.consent_given);
    }

    #[tokio::test]
    async fn auto_granted_consent_is_reset_once_for_upgrades() {
        let _guard = SETTINGS_ENV_LOCK.lock().unwrap();

        let legacy = serde_json::json!({
            "recording_notifications": true,
            "time_based_reminders": true,
            "meeting_reminders": true,
            "respect_do_not_disturb": true,
            "notification_sound": true,
            "system_permission_granted": true,
            "consent_given": true,
            "manual_dnd_mode": false,
            "notification_preferences": {
                "show_recording_started": true,
                "show_recording_stopped": true,
                "show_recording_paused": true,
                "show_recording_resumed": true,
                "show_transcription_complete": true,
                "show_meeting_reminders": true,
                "show_system_errors": true,
                "meeting_reminder_minutes": [15, 5]
            }
        })
        .to_string();

        let app = test_manager(Some(&legacy)).await;
        let settings = app.manager.get_settings().await;

        assert!(!settings.consent_given, "auto-granted consent is revoked");
        assert!(!settings.system_permission_granted);
        assert!(settings.consent_migration_v1, "marker is written");

        assert!(settings.notification_preferences.show_recording_started);
    }

    #[tokio::test]
    async fn consent_survives_a_reload_once_the_marker_is_set() {
        let _guard = SETTINGS_ENV_LOCK.lock().unwrap();
        let dir = tempfile::tempdir().expect("tempdir");
        let path = dir.path().join("notifications.json");
        std::env::set_var("AFTERWORD_TEST_NOTIFICATION_SETTINGS", &path);

        {
            let app = tauri::test::mock_app();
            let manager = NotificationManager::new(app.handle().clone())
                .await
                .unwrap();
            manager.set_consent(true).await.unwrap();
        }

        let app = tauri::test::mock_app();
        let manager = NotificationManager::new(app.handle().clone())
            .await
            .unwrap();
        let settings = manager.get_settings().await;

        assert!(settings.consent_given, "migration runs only once");
        assert!(settings.consent_migration_v1);
    }

    #[test]
    fn preserve_consent_state_keeps_only_consent_fields() {
        let mut current = NotificationSettings::default();
        current.consent_given = true;
        current.system_permission_granted = true;
        current.consent_migration_v1 = true;
        current.notification_sound = true;

        let mut incoming = NotificationSettings::default();
        incoming.consent_given = false;
        incoming.system_permission_granted = false;
        incoming.consent_migration_v1 = false;
        incoming.notification_sound = false;

        let merged = crate::notifications::settings::preserve_consent_state(&current, incoming);

        assert!(merged.consent_given);
        assert!(merged.system_permission_granted);
        assert!(merged.consent_migration_v1);
        assert!(
            !merged.notification_sound,
            "other fields come from the payload"
        );
    }
}
