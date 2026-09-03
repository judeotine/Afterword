#![cfg_attr(
    all(not(debug_assertions), target_os = "windows"),
    windows_subsystem = "windows"
)]

use log;

fn main() {
    // Logging is initialized by tauri-plugin-log in app_lib::run().
    log::info!("Starting application...");
    app_lib::run();
}
