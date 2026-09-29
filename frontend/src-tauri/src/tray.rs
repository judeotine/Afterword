use tauri::{
    menu::{MenuBuilder, MenuItemBuilder, PredefinedMenuItem},
    tray::TrayIconBuilder,
    AppHandle, Emitter, Manager, Runtime,
};

#[derive(Debug, Clone)]
pub enum RecordingState {
    Stopped,
    Starting,
    Recording,
    Pausing,
    Paused,
    Resuming,
    Stopping,
}

pub fn create_tray<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<()> {
    let menu = build_menu(app, RecordingState::Stopped, true)?;

    TrayIconBuilder::with_id("main-tray")
        .menu(&menu)
        .tooltip("Afterword")
        .icon(app.default_window_icon().unwrap().clone())
        .on_menu_event(|app, event| handle_menu_event(app, event.id.as_ref()))
        .build(app)?;

    update_tray_menu(app);

    Ok(())
}

fn handle_menu_event<R: Runtime>(app: &AppHandle<R>, item_id: &str) {
    match item_id {
        "toggle_recording" => toggle_recording_handler(app),
        "pause_recording" => pause_recording_handler(app),
        "resume_recording" => resume_recording_handler(app),
        "stop_recording" => stop_recording_handler(app),
        "open_window" => focus_main_window(app),
        "settings" => {
            focus_main_window(app);
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.eval("window.location.assign('/settings')");
            }
        }
        "check_updates" => check_updates_handler(app),
        "quit" => app.exit(0),
        _ => {}
    }
}
fn toggle_recording_handler<R: Runtime>(app: &AppHandle<R>) {
    focus_main_window(app);
    let app_clone = app.clone();
    tauri::async_runtime::spawn(async move {
        if crate::is_recording().await {
            set_tray_state(&app_clone, RecordingState::Stopping);

            log::info!("Tray toggle: Stopping recording...");

            let data_dir = match app_clone.path().app_data_dir() {
                Ok(dir) => dir,
                Err(e) => {
                    log::error!("Failed to get app data dir: {}", e);
                    update_tray_menu_async(&app_clone).await;
                    return;
                }
            };

            let timestamp = chrono::Local::now().format("%Y-%m-%dT%H-%M-%S").to_string();
            let save_path = data_dir.join(format!("recording-{}.wav", timestamp));

            let stop_result = crate::audio::recording_commands::stop_recording(
                app_clone.clone(),
                crate::audio::recording_commands::RecordingArgs {
                    save_path: save_path.to_string_lossy().to_string(),
                },
            )
            .await;

            match stop_result {
                Ok(_) => {
                    log::info!("Tray toggle: Recording stopped successfully");

                    if let Err(e) = app_clone.emit("recording-stop-complete", true) {
                        log::error!(
                            "Tray toggle: Failed to emit recording-stop-complete event: {}",
                            e
                        );
                    }
                }
                Err(e) => {
                    log::error!("Tray toggle: Failed to stop recording: {}", e);

                    update_tray_menu_async(&app_clone).await;
                }
            }
        } else {
            set_tray_state(&app_clone, RecordingState::Starting);

            log::info!("Emitting start recording event from tray");
            if let Some(window) = app_clone.get_webview_window("main") {
                let _ = window.eval("sessionStorage.setItem('autoStartRecording', 'true')");
                let _ = window.eval("window.location.assign('/')");
            }
        }
    });
}

fn pause_recording_handler<R: Runtime>(app: &AppHandle<R>) {
    set_tray_state(app, RecordingState::Pausing);

    let app_clone = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(e) = crate::audio::recording_commands::pause_recording(app_clone.clone()).await {
            log::error!("Failed to pause recording from tray: {}", e);

            update_tray_menu_async(&app_clone).await;
        } else {
            log::info!("Recording paused from tray");
        }
    });
}

fn resume_recording_handler<R: Runtime>(app: &AppHandle<R>) {
    set_tray_state(app, RecordingState::Resuming);

    let app_clone = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(e) = crate::audio::recording_commands::resume_recording(app_clone.clone()).await
        {
            log::error!("Failed to resume recording from tray: {}", e);

            update_tray_menu_async(&app_clone).await;
        } else {
            log::info!("Recording resumed from tray");
        }
    });
}

fn stop_recording_handler<R: Runtime>(app: &AppHandle<R>) {
    set_tray_state(app, RecordingState::Stopping);

    focus_main_window(app);
    let app_clone = app.clone();
    tauri::async_runtime::spawn(async move {
        log::info!("Tray: Stopping recording...");

        let data_dir = match app_clone.path().app_data_dir() {
            Ok(dir) => dir,
            Err(e) => {
                log::error!("Failed to get app data dir: {}", e);
                update_tray_menu_async(&app_clone).await;
                return;
            }
        };

        let timestamp = chrono::Local::now().format("%Y-%m-%dT%H-%M-%S").to_string();
        let save_path = data_dir.join(format!("recording-{}.wav", timestamp));

        let stop_result = crate::audio::recording_commands::stop_recording(
            app_clone.clone(),
            crate::audio::recording_commands::RecordingArgs {
                save_path: save_path.to_string_lossy().to_string(),
            },
        )
        .await;

        match stop_result {
            Ok(_) => {
                log::info!("Tray: Recording stopped successfully");

                if let Err(e) = app_clone.emit("recording-stop-complete", true) {
                    log::error!("Tray: Failed to emit recording-stop-complete event: {}", e);
                }
            }
            Err(e) => {
                log::error!("Tray: Failed to stop recording: {}", e);

                update_tray_menu_async(&app_clone).await;
            }
        }
    });
}

fn check_updates_handler<R: Runtime>(app: &AppHandle<R>) {
    focus_main_window(app);
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.eval("window.dispatchEvent(new CustomEvent('check-updates-from-tray'))");
    }
}

pub fn update_tray_menu<R: Runtime>(app: &AppHandle<R>) {
    let app_clone = app.clone();
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        update_tray_menu_async(&app_clone).await;
    });
}

pub fn set_tray_state<R: Runtime>(app: &AppHandle<R>, state: RecordingState) {
    log::info!("Tray: Setting intermediate state: {:?}", state);

    if let Ok(menu) = build_menu(app, state, true) {
        if let Some(tray) = app.tray_by_id("main-tray") {
            let result = tray.set_menu(Some(menu));
            log::info!("Tray: Intermediate state menu update result: {:?}", result);
        } else {
            log::warn!("Tray: Could not find tray with id 'main-tray'");
        }
    } else {
        log::error!("Tray: Failed to build menu for intermediate state");
    }
}

async fn get_current_recording_state() -> RecordingState {
    let is_recording = crate::audio::recording_commands::is_recording().await;
    log::info!(
        "Tray: get_current_recording_state - is_recording: {}",
        is_recording
    );

    if !is_recording {
        log::info!("Tray: Recording state is Stopped");
        return RecordingState::Stopped;
    }

    let is_paused = crate::audio::recording_commands::is_recording_paused().await;
    log::info!("Tray: is_paused: {}", is_paused);

    if is_paused {
        log::info!("Tray: Recording state is Paused");
        RecordingState::Paused
    } else {
        log::info!("Tray: Recording state is Recording");
        RecordingState::Recording
    }
}

async fn check_can_record<R: Runtime>(app: &AppHandle<R>) -> bool {
    let onboarding_complete = match crate::onboarding::load_onboarding_status(app).await {
        Ok(status) => status.completed,
        Err(e) => {
            log::warn!(
                "Tray: Failed to load onboarding status: {}, assuming complete",
                e
            );
            true
        }
    };

    if onboarding_complete {
        return true;
    }

    match crate::parakeet_engine::commands::parakeet_has_available_models().await {
        Ok(has_models) => has_models,
        Err(e) => {
            log::warn!(
                "Tray: Failed to check Parakeet models: {}, assuming not ready",
                e
            );
            false
        }
    }
}

pub async fn update_tray_menu_async<R: Runtime>(app: &AppHandle<R>) {
    log::info!("Tray: update_tray_menu_async called");

    let recording_state = get_current_recording_state().await;
    log::info!("Tray: Current recording state: {:?}", recording_state);

    let can_record = check_can_record(app).await;
    log::info!("Tray: can_record: {}", can_record);

    if let Ok(menu) = build_menu(app, recording_state, can_record) {
        if let Some(tray) = app.tray_by_id("main-tray") {
            let result = tray.set_menu(Some(menu));
            log::info!("Tray: Menu update result: {:?}", result);
        } else {
            log::warn!("Tray: Could not find tray with id 'main-tray'");
        }
    } else {
        log::error!("Tray: Failed to build menu");
    }
}

fn build_menu<R: Runtime>(
    app: &AppHandle<R>,
    state: RecordingState,
    can_record: bool,
) -> tauri::Result<tauri::menu::Menu<R>> {
    let mut builder = MenuBuilder::new(app);

    if !can_record {
        builder = builder.item(
            &MenuItemBuilder::new("⏳ Downloading transcription model...")
                .enabled(false)
                .build(app)?,
        );
    } else {
        match state {
            RecordingState::Stopped => {
                builder = builder.item(
                    &MenuItemBuilder::with_id("toggle_recording", "Start Recording").build(app)?,
                );
            }
            RecordingState::Starting => {
                builder = builder.item(
                    &MenuItemBuilder::new("🔄 Starting Recording...")
                        .enabled(false)
                        .build(app)?,
                );
            }
            RecordingState::Recording => {
                builder = builder
                    .item(
                        &MenuItemBuilder::with_id("pause_recording", "⏸ Pause Recording")
                            .build(app)?,
                    )
                    .item(
                        &MenuItemBuilder::with_id("stop_recording", "⏹ Stop Recording")
                            .build(app)?,
                    );
            }
            RecordingState::Pausing => {
                builder = builder
                    .item(
                        &MenuItemBuilder::new("⏸ Pausing...")
                            .enabled(false)
                            .build(app)?,
                    )
                    .item(
                        &MenuItemBuilder::with_id("stop_recording", "⏹ Stop Recording")
                            .build(app)?,
                    );
            }
            RecordingState::Paused => {
                builder = builder
                    .item(
                        &MenuItemBuilder::with_id("resume_recording", "▶ Resume Recording")
                            .build(app)?,
                    )
                    .item(
                        &MenuItemBuilder::with_id("stop_recording", "⏹ Stop Recording")
                            .build(app)?,
                    );
            }
            RecordingState::Resuming => {
                builder = builder
                    .item(
                        &MenuItemBuilder::new("▶ Resuming...")
                            .enabled(false)
                            .build(app)?,
                    )
                    .item(
                        &MenuItemBuilder::with_id("stop_recording", "⏹ Stop Recording")
                            .build(app)?,
                    );
            }
            RecordingState::Stopping => {
                builder = builder.item(
                    &MenuItemBuilder::new("⏹ Stopping...")
                        .enabled(false)
                        .build(app)?,
                );
            }
        }
    }

    builder
        .item(&PredefinedMenuItem::separator(app)?)
        .item(&MenuItemBuilder::with_id("open_window", "Open Main Window").build(app)?)
        .item(&MenuItemBuilder::with_id("settings", "Settings").build(app)?)
        .item(&MenuItemBuilder::with_id("check_updates", "Check for Updates").build(app)?)
        .item(&PredefinedMenuItem::separator(app)?)
        .item(&MenuItemBuilder::with_id("quit", "Quit").build(app)?)
        .build()
}

pub(crate) fn focus_main_window<R: Runtime>(app: &AppHandle<R>) {
    if let Some(window) = app.get_webview_window("main") {
        if let Err(e) = window.unminimize() {
            log::error!("Failed to unminimize main window: {}", e);
        }

        if let Err(e) = window.show() {
            log::error!("Failed to show main window: {}", e);
        }

        if let Err(e) = window.set_focus() {
            log::error!("Failed to focus main window: {}", e);
        }

        if let Err(e) = window.eval("window.focus()") {
            log::error!("Failed to focus main webview: {}", e);
        }
    } else {
        log::warn!("Could not find main window");
    }
}
