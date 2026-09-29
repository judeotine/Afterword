use std::path::PathBuf;

use tauri::{AppHandle, Manager, Runtime};

use super::screen_recorder;

fn default_screen_output(app: &AppHandle<impl Runtime>) -> PathBuf {
    let stamp = chrono::Local::now().format("%Y%m%d_%H%M%S");
    let name = format!("screen_{}.mp4", stamp);
    match app.path().app_data_dir() {
        Ok(dir) => dir.join("recordings").join(name),
        Err(_) => PathBuf::from(name),
    }
}

#[tauri::command]
pub async fn start_screen_recording<R: Runtime>(
    app: AppHandle<R>,
    output_path: Option<String>,
    screen_index: Option<u32>,
    audio_index: Option<u32>,
) -> Result<String, String> {
    let path = match output_path {
        Some(value) if !value.trim().is_empty() => PathBuf::from(value),
        _ => default_screen_output(&app),
    };
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| e.to_string())?;
    }
    screen_recorder::start_screen_recording(path.clone(), screen_index, audio_index)
        .map_err(|e| e.to_string())?;
    Ok(path.to_string_lossy().to_string())
}

#[tauri::command]
pub async fn stop_screen_recording() -> Result<Option<String>, String> {
    let path = screen_recorder::stop_screen_recording().map_err(|e| e.to_string())?;
    Ok(path.map(|p| p.to_string_lossy().to_string()))
}

#[tauri::command]
pub async fn is_screen_recording() -> bool {
    screen_recorder::is_screen_recording()
}
