pub mod backend_config;
pub mod microphone;
pub mod system;

#[cfg(target_os = "macos")]
pub mod core_audio;

#[cfg(target_os = "linux")]
pub mod pulse_monitor;

pub use system::{
    check_system_audio_permissions, list_system_audio_devices, start_system_audio_capture,
    SystemAudioCapture, SystemAudioStream,
};

#[cfg(target_os = "macos")]
pub use core_audio::{CoreAudioCapture, CoreAudioStream};

#[cfg(target_os = "linux")]
pub use pulse_monitor::{PulseMonitorCapture, PulseMonitorStream, DEFAULT_MONITOR_SOURCE};

pub use backend_config::{
    get_available_backends, get_current_backend, set_current_backend, AudioCaptureBackend,
    BackendConfig, BACKEND_CONFIG,
};
