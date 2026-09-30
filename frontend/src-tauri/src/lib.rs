use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Mutex as StdMutex;

pub(crate) use afterword_core::perf_debug;

pub mod analytics;
pub mod account;
pub mod api;
pub mod audio;
pub use afterword_core::config;
pub mod anthropic;
pub mod console_utils;
pub mod database;
pub mod groq;
pub mod notifications;
pub mod ollama;
pub mod onboarding;
pub mod openai;
pub mod openrouter;
pub mod parakeet_engine;
pub mod state;
pub mod summary;
pub mod tray;
pub mod utils;
pub mod whisper_engine;

use audio::{list_audio_devices, trigger_audio_permission, AudioDevice};
use log::{error as log_error, info as log_info};
use notifications::commands::NotificationManagerState;
use std::sync::Arc;
use tauri::{AppHandle, Manager, Runtime};
use tokio::sync::RwLock;

static RECORDING_FLAG: AtomicBool = AtomicBool::new(false);

static LANGUAGE_PREFERENCE: std::sync::LazyLock<StdMutex<String>> =
    std::sync::LazyLock::new(|| StdMutex::new("auto-translate".to_string()));

#[derive(Debug, Deserialize)]
struct RecordingArgs {
    save_path: String,
}

#[derive(Debug, Serialize, Clone)]
struct TranscriptionStatus {
    chunks_in_queue: usize,
    is_processing: bool,
    last_activity_ms: u64,
}

#[tauri::command]
async fn start_recording<R: Runtime>(
    app: AppHandle<R>,
    mic_device_name: Option<String>,
    system_device_name: Option<String>,
    meeting_name: Option<String>,
) -> Result<(), String> {
    log_info!("🔥 CALLED start_recording with meeting: {:?}", meeting_name);
    log_info!(
        "📋 Backend received parameters - mic: {:?}, system: {:?}, meeting: {:?}",
        mic_device_name,
        system_device_name,
        meeting_name
    );

    if is_recording().await {
        return Err("Recording already in progress".to_string());
    }

    match audio::recording_commands::start_recording_with_devices_and_meeting(
        app.clone(),
        mic_device_name,
        system_device_name,
        meeting_name.clone(),
    )
    .await
    {
        Ok(_) => {
            RECORDING_FLAG.store(true, Ordering::SeqCst);
            tray::update_tray_menu(&app);

            log_info!("Recording started successfully");

            let notification_manager_state = app.state::<NotificationManagerState<R>>();
            if let Err(e) = notifications::commands::show_recording_started_notification(
                &app,
                &notification_manager_state,
                meeting_name.clone(),
            )
            .await
            {
                log_error!("Failed to show recording started notification: {}", e);
            } else {
                log_info!("Successfully showed recording started notification");
            }

            Ok(())
        }
        Err(e) => {
            log_error!("Failed to start audio recording: {}", e);
            Err(format!("Failed to start recording: {}", e))
        }
    }
}

#[tauri::command]
async fn stop_recording<R: Runtime>(app: AppHandle<R>, args: RecordingArgs) -> Result<(), String> {
    log_info!("Attempting to stop recording...");

    if !audio::recording_commands::is_recording().await {
        log_info!("Recording is already stopped");
        return Ok(());
    }

    match audio::recording_commands::stop_recording(
        app.clone(),
        audio::recording_commands::RecordingArgs {
            save_path: args.save_path.clone(),
        },
    )
    .await
    {
        Ok(_) => {
            RECORDING_FLAG.store(false, Ordering::SeqCst);
            tray::update_tray_menu(&app);

            if let Some(parent) = std::path::Path::new(&args.save_path).parent() {
                if !parent.exists() {
                    log_info!("Creating directory: {:?}", parent);
                    if let Err(e) = std::fs::create_dir_all(parent) {
                        let err_msg = format!("Failed to create save directory: {}", e);
                        log_error!("{}", err_msg);
                        return Err(err_msg);
                    }
                }
            }

            let notification_manager_state = app.state::<NotificationManagerState<R>>();
            if let Err(e) = notifications::commands::show_recording_stopped_notification(
                &app,
                &notification_manager_state,
            )
            .await
            {
                log_error!("Failed to show recording stopped notification: {}", e);
            } else {
                log_info!("Successfully showed recording stopped notification");
            }

            Ok(())
        }
        Err(e) => {
            log_error!("Failed to stop audio recording: {}", e);

            RECORDING_FLAG.store(false, Ordering::SeqCst);
            tray::update_tray_menu(&app);
            Err(format!("Failed to stop recording: {}", e))
        }
    }
}

#[tauri::command]
async fn is_recording() -> bool {
    audio::recording_commands::is_recording().await
}

#[tauri::command]
fn get_transcription_status() -> TranscriptionStatus {
    TranscriptionStatus {
        chunks_in_queue: 0,
        is_processing: false,
        last_activity_ms: 0,
    }
}

#[tauri::command]
fn read_audio_file(file_path: String) -> Result<Vec<u8>, String> {
    match std::fs::read(&file_path) {
        Ok(data) => Ok(data),
        Err(e) => Err(format!("Failed to read audio file: {}", e)),
    }
}

#[tauri::command]
async fn save_transcript(file_path: String, content: String) -> Result<(), String> {
    log_info!("Saving transcript to: {}", file_path);

    if let Some(parent) = std::path::Path::new(&file_path).parent() {
        if !parent.exists() {
            std::fs::create_dir_all(parent)
                .map_err(|e| format!("Failed to create directory: {}", e))?;
        }
    }

    std::fs::write(&file_path, content)
        .map_err(|e| format!("Failed to write transcript: {}", e))?;

    log_info!("Transcript saved successfully");
    Ok(())
}

#[tauri::command]
async fn start_audio_level_monitoring<R: Runtime>(
    app: AppHandle<R>,
    device_names: Vec<String>,
) -> Result<(), String> {
    log_info!(
        "Starting audio level monitoring for devices: {:?}",
        device_names
    );

    audio::simple_level_monitor::start_monitoring(app, device_names)
        .await
        .map_err(|e| format!("Failed to start audio level monitoring: {}", e))
}

#[tauri::command]
async fn stop_audio_level_monitoring() -> Result<(), String> {
    log_info!("Stopping audio level monitoring");

    audio::simple_level_monitor::stop_monitoring()
        .await
        .map_err(|e| format!("Failed to stop audio level monitoring: {}", e))
}

#[tauri::command]
async fn is_audio_level_monitoring() -> bool {
    audio::simple_level_monitor::is_monitoring()
}

#[tauri::command]
async fn get_audio_devices() -> Result<Vec<AudioDevice>, String> {
    list_audio_devices()
        .await
        .map_err(|e| format!("Failed to list audio devices: {}", e))
}

#[tauri::command]
async fn trigger_microphone_permission() -> Result<bool, String> {
    trigger_audio_permission()
        .map_err(|e| format!("Failed to trigger microphone permission: {}", e))
}

#[tauri::command]
async fn start_recording_with_devices<R: Runtime>(
    app: AppHandle<R>,
    mic_device_name: Option<String>,
    system_device_name: Option<String>,
) -> Result<(), String> {
    start_recording_with_devices_and_meeting(app, mic_device_name, system_device_name, None).await
}

#[tauri::command]
async fn start_recording_with_devices_and_meeting<R: Runtime>(
    app: AppHandle<R>,
    mic_device_name: Option<String>,
    system_device_name: Option<String>,
    meeting_name: Option<String>,
) -> Result<(), String> {
    log_info!("🚀 CALLED start_recording_with_devices_and_meeting - Mic: {:?}, System: {:?}, Meeting: {:?}",
             mic_device_name, system_device_name, meeting_name);

    let meeting_name_for_notification = meeting_name.clone();

    let recording_result = match (mic_device_name.clone(), system_device_name.clone()) {
        (None, None) => {
            log_info!(
                "No devices specified, starting with defaults and meeting: {:?}",
                meeting_name
            );
            audio::recording_commands::start_recording_with_meeting_name(app.clone(), meeting_name)
                .await
        }
        _ => {
            log_info!(
                "Starting with specified devices: mic={:?}, system={:?}, meeting={:?}",
                mic_device_name,
                system_device_name,
                meeting_name
            );
            audio::recording_commands::start_recording_with_devices_and_meeting(
                app.clone(),
                mic_device_name,
                system_device_name,
                meeting_name,
            )
            .await
        }
    };

    match recording_result {
        Ok(_) => {
            log_info!("Recording started successfully via tauri command");

            let notification_manager_state = app.state::<NotificationManagerState<R>>();
            if let Err(e) = notifications::commands::show_recording_started_notification(
                &app,
                &notification_manager_state,
                meeting_name_for_notification.clone(),
            )
            .await
            {
                log_error!("Failed to show recording started notification: {}", e);
            }

            Ok(())
        }
        Err(e) => {
            log_error!("Failed to start recording via tauri command: {}", e);
            Err(e)
        }
    }
}

#[tauri::command]
async fn set_language_preference(language: String) -> Result<(), String> {
    let mut lang_pref = LANGUAGE_PREFERENCE
        .lock()
        .map_err(|e| format!("Failed to set language preference: {}", e))?;
    log_info!("Setting language preference to: {}", language);
    *lang_pref = language;
    Ok(())
}

pub fn get_language_preference_internal() -> Option<String> {
    LANGUAGE_PREFERENCE.lock().ok().map(|lang| lang.clone())
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct LogDirectives {
    global: log::LevelFilter,

    targets: Vec<(String, log::LevelFilter)>,

    ignored: Vec<String>,
}

impl Default for LogDirectives {
    fn default() -> Self {
        Self {
            global: log::LevelFilter::Info,
            targets: Vec::new(),
            ignored: Vec::new(),
        }
    }
}

impl LogDirectives {
    fn max_level(&self) -> log::LevelFilter {
        self.targets
            .iter()
            .map(|(_, level)| *level)
            .chain(std::iter::once(self.global))
            .max()
            .unwrap_or(self.global)
    }
}

fn parse_rust_log(value: &str) -> LogDirectives {
    let mut directives = LogDirectives::default();

    for raw in value.split(',') {
        let directive = raw.trim();
        if directive.is_empty() {
            continue;
        }

        match directive.split_once('=') {
            Some((target, level)) => {
                let target = target.trim();
                match (target.is_empty(), level.trim().parse::<log::LevelFilter>()) {
                    (false, Ok(level)) => directives.targets.push((target.to_string(), level)),
                    _ => directives.ignored.push(directive.to_string()),
                }
            }
            None => match directive.parse::<log::LevelFilter>() {
                Ok(level) => directives.global = level,
                Err(_) => directives.ignored.push(directive.to_string()),
            },
        }
    }

    directives
}

fn configured_log_directives() -> LogDirectives {
    match std::env::var("RUST_LOG") {
        Ok(value) => parse_rust_log(&value),
        Err(_) => LogDirectives::default(),
    }
}

pub fn run() {
    let log_directives = configured_log_directives();

    for ignored in &log_directives.ignored {
        eprintln!(
            "warning: ignoring unrecognized RUST_LOG directive `{}`",
            ignored
        );
    }
    log::set_max_level(log_directives.max_level());

    let mut builder = tauri::Builder::default();

    #[cfg(any(target_os = "macos", windows, target_os = "linux"))]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, args, cwd| {
            log_info!(
                "Second app instance requested with args: {:?}, cwd: {:?}",
                args,
                cwd
            );

            tray::focus_main_window(app);
        }));
    }

    let mut log_plugin = tauri_plugin_log::Builder::new()
        .targets([
            tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::Stdout),
            tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::LogDir {
                file_name: Some("afterword".into()),
            }),
        ])
        .level(log_directives.global)
        .max_file_size(10_000_000)
        .rotation_strategy(tauri_plugin_log::RotationStrategy::KeepAll);

    for (target, level) in &log_directives.targets {
        log_plugin = log_plugin.level_for(target.clone(), *level);
    }

    builder
        .plugin(log_plugin.build())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_store::Builder::default().build())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .manage(Arc::new(RwLock::new(
            None::<notifications::manager::NotificationManager<tauri::Wry>>,
        )) as NotificationManagerState<tauri::Wry>)
        .manage(audio::init_system_audio_state())
        .manage(summary::summary_engine::ModelManagerState(Arc::new(
            tokio::sync::Mutex::new(None),
        )))
        .setup(|_app| {
            log::info!("Application setup complete");

            if let Err(e) = tray::create_tray(_app.handle()) {
                log::error!("Failed to create system tray: {}", e);
            }

            log::info!("Initializing notification system...");
            let app_for_notif = _app.handle().clone();
            tauri::async_runtime::spawn(async move {
                let notif_state = app_for_notif.state::<NotificationManagerState<tauri::Wry>>();
                match notifications::commands::initialize_notification_manager(
                    app_for_notif.clone(),
                )
                .await
                {
                    Ok(manager) => {
                        let mut state_lock = notif_state.write().await;
                        *state_lock = Some(manager);
                        log::info!("Notification system initialized");
                    }
                    Err(e) => {
                        log::error!("Failed to initialize notification manager: {}", e);
                    }
                }
            });

            whisper_engine::commands::set_models_directory(&_app.handle());

            tauri::async_runtime::spawn(async {
                if let Err(e) = whisper_engine::commands::whisper_init().await {
                    log::error!("Failed to initialize Whisper engine on startup: {}", e);
                }
            });

            parakeet_engine::commands::set_models_directory(&_app.handle());

            tauri::async_runtime::spawn(async {
                if let Err(e) = parakeet_engine::commands::parakeet_init().await {
                    log::error!("Failed to initialize Parakeet engine on startup: {}", e);
                }
            });

            let app_handle_for_model_manager = _app.handle().clone();
            tauri::async_runtime::spawn(async move {
                match summary::summary_engine::commands::init_model_manager_at_startup(
                    &app_handle_for_model_manager,
                )
                .await
                {
                    Ok(_) => log::info!("ModelManager initialized successfully at startup"),
                    Err(e) => {
                        log::warn!("Failed to initialize ModelManager at startup: {}", e);
                        log::warn!("ModelManager will be lazy-initialized on first use");
                    }
                }
            });

            tauri::async_runtime::block_on(async {
                database::setup::initialize_database_on_startup(&_app.handle()).await
            })
            .expect("Failed to initialize database");

            log::info!("Initializing bundled templates directory...");
            if let Ok(resource_path) = _app.handle().path().resource_dir() {
                let templates_dir = resource_path.join("templates");
                log::info!(
                    "Setting bundled templates directory to: {:?}",
                    templates_dir
                );
                summary::templates::set_bundled_templates_dir(templates_dir);
            } else {
                log::warn!("Failed to resolve resource directory for templates");
            }

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    api.prevent_close();
                    if let Err(e) = window.hide() {
                        log::error!("Failed to hide main window on close request: {}", e);
                    } else {
                        log::info!("Main window hidden to tray on close request");
                    }
                }
            }
        })
        .invoke_handler(tauri::generate_handler![
            start_recording,
            stop_recording,
            is_recording,
            get_transcription_status,
            read_audio_file,
            save_transcript,
            account::commands::account_store_refresh_token,
            account::commands::account_get_refresh_token,
            account::commands::account_clear_refresh_token,
            account::google::account_google_sign_in,
            account::sync::sync_enqueue,
            account::sync::sync_pending,
            account::sync::sync_mark_done,
            account::sync::sync_mark_failed,
            account::sync::sync_status_counts,
            audio::screen_commands::start_screen_recording,
            audio::screen_commands::stop_screen_recording,
            audio::screen_commands::is_screen_recording,
            analytics::commands::init_analytics,
            analytics::commands::disable_analytics,
            analytics::commands::track_event,
            analytics::commands::identify_user,
            analytics::commands::track_meeting_started,
            analytics::commands::track_recording_started,
            analytics::commands::track_recording_stopped,
            analytics::commands::track_meeting_deleted,
            analytics::commands::track_settings_changed,
            analytics::commands::track_feature_used,
            analytics::commands::is_analytics_enabled,
            analytics::commands::start_analytics_session,
            analytics::commands::end_analytics_session,
            analytics::commands::track_daily_active_user,
            analytics::commands::track_user_first_launch,
            analytics::commands::is_analytics_session_active,
            analytics::commands::track_summary_generation_started,
            analytics::commands::track_summary_generation_completed,
            analytics::commands::track_summary_regenerated,
            analytics::commands::track_model_changed,
            analytics::commands::track_custom_prompt_used,
            analytics::commands::track_meeting_ended,
            analytics::commands::track_analytics_enabled,
            analytics::commands::track_analytics_disabled,
            analytics::commands::track_analytics_transparency_viewed,
            whisper_engine::commands::whisper_init,
            whisper_engine::commands::whisper_get_available_models,
            whisper_engine::commands::whisper_load_model,
            whisper_engine::commands::whisper_get_current_model,
            whisper_engine::commands::whisper_is_model_loaded,
            whisper_engine::commands::whisper_has_available_models,
            whisper_engine::commands::whisper_validate_model_ready,
            whisper_engine::commands::whisper_transcribe_audio,
            whisper_engine::commands::whisper_get_models_directory,
            whisper_engine::commands::whisper_download_model,
            whisper_engine::commands::whisper_cancel_download,
            whisper_engine::commands::whisper_delete_corrupted_model,
            parakeet_engine::commands::parakeet_init,
            parakeet_engine::commands::parakeet_get_available_models,
            parakeet_engine::commands::parakeet_load_model,
            parakeet_engine::commands::parakeet_get_current_model,
            parakeet_engine::commands::parakeet_is_model_loaded,
            parakeet_engine::commands::parakeet_has_available_models,
            parakeet_engine::commands::parakeet_validate_model_ready,
            parakeet_engine::commands::parakeet_transcribe_audio,
            parakeet_engine::commands::parakeet_get_models_directory,
            parakeet_engine::commands::parakeet_download_model,
            parakeet_engine::commands::parakeet_retry_download,
            parakeet_engine::commands::parakeet_cancel_download,
            parakeet_engine::commands::parakeet_delete_corrupted_model,
            parakeet_engine::commands::open_parakeet_models_folder,
            get_audio_devices,
            trigger_microphone_permission,
            start_recording_with_devices,
            start_recording_with_devices_and_meeting,
            start_audio_level_monitoring,
            stop_audio_level_monitoring,
            is_audio_level_monitoring,
            audio::recording_commands::pause_recording,
            audio::recording_commands::resume_recording,
            audio::recording_commands::is_recording_paused,
            audio::recording_commands::get_recording_state,
            audio::recording_commands::get_meeting_folder_path,
            audio::recording_commands::get_transcript_history,
            audio::recording_commands::get_recording_meeting_name,
            audio::recording_commands::poll_audio_device_events,
            audio::recording_commands::get_reconnection_status,
            audio::recording_commands::attempt_device_reconnect,
            audio::recording_commands::get_active_audio_output,
            audio::incremental_saver::recover_audio_from_checkpoints,
            audio::incremental_saver::cleanup_checkpoints,
            audio::incremental_saver::has_audio_checkpoints,
            console_utils::show_console,
            console_utils::hide_console,
            console_utils::toggle_console,
            ollama::get_ollama_models,
            ollama::pull_ollama_model,
            ollama::delete_ollama_model,
            ollama::get_ollama_model_context,
            openai::openai::get_openai_models,
            anthropic::anthropic::get_anthropic_models,
            groq::groq::get_groq_models,
            api::api_get_meetings,
            api::api_search_transcripts,
            api::api_get_model_config,
            api::api_save_model_config,
            api::api_get_api_key,
            api::api_get_transcript_config,
            api::api_save_transcript_config,
            api::api_get_transcript_api_key,
            api::api_delete_meeting,
            api::api_get_meeting,
            api::api_get_meeting_metadata,
            api::api_get_meeting_transcripts,
            api::api_save_meeting_title,
            api::api_save_transcript,
            api::open_meeting_folder,
            api::open_external_url,
            api::api_save_custom_openai_config,
            api::api_get_custom_openai_config,
            api::api_test_custom_openai_connection,
            summary::commands::api_process_transcript,
            summary::commands::api_get_summary,
            summary::commands::api_save_meeting_summary,
            summary::commands::api_get_meeting_summary_language,
            summary::commands::api_save_meeting_summary_language,
            summary::commands::api_get_meeting_detected_summary_language,
            summary::commands::api_save_meeting_detected_summary_language,
            summary::commands::api_detect_transcript_summary_language,
            summary::commands::api_cancel_summary,
            summary::template_commands::api_list_templates,
            summary::template_commands::api_get_template_details,
            summary::template_commands::api_validate_template,
            summary::summary_engine::commands::builtin_ai_list_models,
            summary::summary_engine::commands::builtin_ai_get_model_info,
            summary::summary_engine::commands::builtin_ai_download_model,
            summary::summary_engine::commands::builtin_ai_cancel_download,
            summary::summary_engine::commands::builtin_ai_delete_model,
            summary::summary_engine::commands::builtin_ai_is_model_ready,
            summary::summary_engine::commands::builtin_ai_get_available_summary_model,
            summary::summary_engine::commands::builtin_ai_get_recommended_model,
            openrouter::get_openrouter_models,
            audio::recording_preferences::get_recording_preferences,
            audio::recording_preferences::set_recording_preferences,
            audio::recording_preferences::get_default_recordings_folder_path,
            audio::recording_preferences::open_recordings_folder,
            audio::recording_preferences::select_recording_folder,
            audio::recording_preferences::get_available_audio_backends,
            audio::recording_preferences::get_current_audio_backend,
            audio::recording_preferences::set_audio_backend,
            audio::recording_preferences::get_audio_backend_info,
            set_language_preference,
            notifications::commands::get_notification_settings,
            notifications::commands::set_notification_settings,
            notifications::commands::request_notification_permission,
            notifications::commands::show_notification,
            notifications::commands::show_test_notification,
            notifications::commands::is_dnd_active,
            notifications::commands::get_system_dnd_status,
            notifications::commands::set_manual_dnd,
            notifications::commands::set_notification_consent,
            notifications::commands::clear_notifications,
            notifications::commands::is_notification_system_ready,
            notifications::commands::initialize_notification_manager_manual,
            notifications::commands::test_notification_with_auto_consent,
            notifications::commands::get_notification_stats,
            audio::system_audio_commands::start_system_audio_capture_command,
            audio::system_audio_commands::list_system_audio_devices_command,
            audio::system_audio_commands::check_system_audio_permissions_command,
            audio::system_audio_commands::start_system_audio_monitoring,
            audio::system_audio_commands::stop_system_audio_monitoring,
            audio::system_audio_commands::get_system_audio_monitoring_status,
            audio::permissions::check_screen_recording_permission_command,
            audio::permissions::request_screen_recording_permission_command,
            audio::permissions::trigger_system_audio_permission_command,
            database::commands::check_first_launch,
            database::commands::select_legacy_database_path,
            database::commands::detect_legacy_database,
            database::commands::check_default_legacy_database,
            database::commands::check_homebrew_database,
            database::commands::import_and_initialize_database,
            database::commands::initialize_fresh_database,
            database::commands::get_database_directory,
            database::commands::open_database_folder,
            whisper_engine::commands::open_models_folder,
            onboarding::get_onboarding_status,
            onboarding::save_onboarding_status_cmd,
            onboarding::reset_onboarding_status_cmd,
            onboarding::complete_onboarding,
            #[cfg(target_os = "macos")]
            utils::open_system_settings,
            audio::retranscription::start_retranscription_command,
            audio::retranscription::cancel_retranscription_command,
            audio::retranscription::is_retranscription_in_progress_command,
            audio::import::select_and_validate_audio_command,
            audio::import::validate_audio_file_command,
            audio::import::start_import_audio_command,
            audio::import::cancel_import_command,
            audio::import::is_import_in_progress_command,
        ])
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|_app_handle, event| match event {
            #[cfg(target_os = "macos")]
            tauri::RunEvent::Reopen { .. } => {
                tray::focus_main_window(_app_handle);
            }
            tauri::RunEvent::Exit => {
                log::info!("Application exiting, cleaning up resources...");
                tauri::async_runtime::block_on(async {
                    if let Some(app_state) = _app_handle.try_state::<state::AppState>() {
                        log::info!("Starting database cleanup...");
                        if let Err(e) = app_state.db_manager.cleanup().await {
                            log::error!("Failed to cleanup database: {}", e);
                        } else {
                            log::info!("Database cleanup completed successfully");
                        }
                    } else {
                        log::warn!(
                            "AppState not available for database cleanup (likely first launch)"
                        );
                    }

                    log::info!("Cleaning up sidecar...");
                    if let Err(e) = summary::summary_engine::force_shutdown_sidecar().await {
                        log::error!("Failed to force shutdown sidecar: {}", e);
                    }
                });
                log::info!("Application cleanup complete");
            }
            _ => {}
        });
}

#[cfg(test)]
mod log_directive_tests {
    use super::{parse_rust_log, LogDirectives};
    use log::LevelFilter;

    #[test]
    fn empty_value_keeps_the_default() {
        assert_eq!(parse_rust_log(""), LogDirectives::default());
        assert_eq!(parse_rust_log("   "), LogDirectives::default());
        assert_eq!(LogDirectives::default().global, LevelFilter::Info);
    }

    #[test]
    fn bare_level_sets_the_global_level() {
        let parsed = parse_rust_log("debug");
        assert_eq!(parsed.global, LevelFilter::Debug);
        assert!(parsed.targets.is_empty());
        assert!(parsed.ignored.is_empty());
        assert_eq!(parsed.max_level(), LevelFilter::Debug);
    }

    #[test]
    fn bare_level_is_case_insensitive() {
        assert_eq!(parse_rust_log("TRACE").global, LevelFilter::Trace);
        assert_eq!(parse_rust_log("Off").global, LevelFilter::Off);
    }

    #[test]
    fn target_directive_becomes_a_per_target_level() {
        let parsed = parse_rust_log("app_lib::audio=debug");
        assert_eq!(parsed.global, LevelFilter::Info);
        assert_eq!(
            parsed.targets,
            vec![("app_lib::audio".to_string(), LevelFilter::Debug)]
        );
        assert!(parsed.ignored.is_empty());

        assert_eq!(parsed.max_level(), LevelFilter::Debug);
    }

    #[test]
    fn directives_are_comma_separated_and_trimmed() {
        let parsed = parse_rust_log(" warn , app_lib::audio = trace ,afterword_core=debug");
        assert_eq!(parsed.global, LevelFilter::Warn);
        assert_eq!(
            parsed.targets,
            vec![
                ("app_lib::audio".to_string(), LevelFilter::Trace),
                ("afterword_core".to_string(), LevelFilter::Debug),
            ]
        );
        assert!(parsed.ignored.is_empty());
        assert_eq!(parsed.max_level(), LevelFilter::Trace);
    }

    #[test]
    fn empty_directives_are_skipped_without_a_warning() {
        let parsed = parse_rust_log("debug,,");
        assert_eq!(parsed.global, LevelFilter::Debug);
        assert!(parsed.ignored.is_empty());
    }

    #[test]
    fn unknown_tokens_are_ignored_and_reported() {
        let parsed = parse_rust_log("info,not_a_level,app_lib::audio=louder,=debug");
        assert_eq!(parsed.global, LevelFilter::Info);
        assert!(parsed.targets.is_empty());
        assert_eq!(
            parsed.ignored,
            vec![
                "not_a_level".to_string(),
                "app_lib::audio=louder".to_string(),
                "=debug".to_string(),
            ]
        );
    }

    #[test]
    fn the_last_bare_level_wins() {
        assert_eq!(parse_rust_log("warn,debug").global, LevelFilter::Debug);
    }

    #[test]
    fn audio_is_no_longer_pinned_to_info() {
        let parsed = parse_rust_log("app_lib::audio=trace");
        assert_eq!(
            parsed.targets,
            vec![("app_lib::audio".to_string(), LevelFilter::Trace)]
        );
    }
}
