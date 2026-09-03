// crates/afterword-core/src/audio/mod.rs
//
// Tauri-free audio processing: decoding, resampling, VAD, encoding and
// hardware capability detection.

pub mod audio_processing;
pub mod constants;
pub mod decoder;
pub mod encode;
pub mod ffmpeg;
pub mod hardware_detector;
pub mod vad;

pub use constants::AUDIO_EXTENSIONS;
pub use decoder::{decode_audio_file, DecodedAudio};
pub use encode::encode_single_audio;
pub use hardware_detector::{AdaptiveWhisperConfig, GpuType, HardwareProfile, PerformanceTier};
pub use vad::extract_speech_16k;
