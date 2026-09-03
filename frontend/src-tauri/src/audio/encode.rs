//! App-side shim for audio encoding.
//!
//! `encode_single_audio` lives in `afterword_core::audio::encode`; only
//! `AudioInput` stays here because it references the app's `AudioDevice`.

use super::AudioDevice;
use std::sync::Arc;

pub use afterword_core::audio::encode::encode_single_audio;

pub struct AudioInput {
    pub data: Arc<Vec<f32>>,
    pub sample_rate: u32,
    pub channels: u16,
    pub device: Arc<AudioDevice>,
}
