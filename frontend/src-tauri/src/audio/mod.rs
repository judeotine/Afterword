pub use afterword_core::audio::constants;
pub use afterword_core::audio::{audio_processing, decoder, ffmpeg, hardware_detector, vad};

pub mod encode;

pub mod capture;
pub mod devices;
pub mod permissions;

pub mod device_detection;
pub mod diagnostics;
pub mod ffmpeg_mixer;
pub mod screen_recorder;

pub mod async_logger;
pub mod batch_processor;
pub mod buffer_pool;
pub mod device_monitor;
pub mod incremental_saver;
pub mod level_monitor;
pub mod pipeline;
pub mod playback_monitor;
pub mod post_processor;
pub mod recording_commands;
pub mod screen_commands;
pub mod recording_manager;
pub mod recording_preferences;
pub mod recording_saver;
pub mod recording_state;
pub mod simple_level_monitor;
pub mod stream;
pub mod system_audio_commands;
pub mod system_detector;

pub mod transcription;

pub(crate) mod common;

pub mod retranscription;

pub mod import;

pub use devices::{
    default_input_device, default_output_device, get_device_and_config, list_audio_devices,
    parse_audio_device, trigger_audio_permission, AudioDevice, AudioTranscriptionEngine,
    DeviceControl, DeviceType, LAST_AUDIO_CAPTURE,
};

pub use capture::{
    check_system_audio_permissions, list_system_audio_devices, start_system_audio_capture,
    SystemAudioCapture, SystemAudioStream,
};

pub use system_detector::{
    new_system_audio_callback, SystemAudioCallback, SystemAudioDetector, SystemAudioEvent,
};

pub use system_audio_commands::{
    check_system_audio_permissions_command, get_system_audio_monitoring_status,
    init_system_audio_state, list_system_audio_devices_command, start_system_audio_capture_command,
    start_system_audio_monitoring, stop_system_audio_monitoring,
};

pub use buffer_pool::{AudioBufferPool, PooledBuffer};
pub use device_monitor::{AudioDeviceMonitor, DeviceEvent, DeviceMonitorType};
pub use encode::{encode_single_audio, AudioInput};
pub use hardware_detector::{AdaptiveWhisperConfig, GpuType, HardwareProfile, PerformanceTier};
pub use level_monitor::{AudioLevelData, AudioLevelMonitor, AudioLevelUpdate};
pub use pipeline::AudioPipelineManager;
pub use post_processor::{PostProcessRequest, PostProcessResponse, PostProcessor};
pub use recording_commands::{
    get_transcription_status, is_recording, start_recording, start_recording_with_devices,
    stop_recording, RecordingArgs, TranscriptUpdate, TranscriptionStatus,
};
pub use recording_manager::RecordingManager;
pub use recording_preferences::{get_default_recordings_folder, RecordingPreferences};
pub use recording_saver::RecordingSaver;
pub use recording_state::{
    AudioChunk, AudioError, DeviceType as RecordingDeviceType, ProcessedAudioChunk, RecordingState,
};
pub use stream::AudioStreamManager;

pub use device_detection::{calculate_buffer_timeout, InputDeviceKind};
pub use diagnostics::{
    log_buffer_health, log_detection_summary, log_device_capabilities, log_mixer_status,
    log_performance_summary,
};

pub use ffmpeg_mixer::{BufferStats, FFmpegAudioMixer, RNNOISE_APPLY_ENABLED};

pub use vad::extract_speech_16k;

pub use decoder::{decode_audio_file, DecodedAudio};

pub use constants::AUDIO_EXTENSIONS;
