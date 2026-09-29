use super::AudioDevice;
use std::sync::Arc;

pub use afterword_core::audio::encode::encode_single_audio;

pub struct AudioInput {
    pub data: Arc<Vec<f32>>,
    pub sample_rate: u32,
    pub channels: u16,
    pub device: Arc<AudioDevice>,
}
