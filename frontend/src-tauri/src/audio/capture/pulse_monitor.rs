use std::pin::Pin;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::task::{Context, Poll, Waker};

use anyhow::{anyhow, Result};
use futures_util::Stream;
use log::{error, info, warn};
use ringbuf::{
    traits::{Consumer, Producer, Split},
    HeapCons, HeapProd, HeapRb,
};

use libpulse_binding::def::BufferAttr;
use libpulse_binding::sample::{Format, Spec};
use libpulse_binding::stream::Direction;
use libpulse_simple_binding::Simple;

pub const DEFAULT_MONITOR_SOURCE: &str = "@DEFAULT_MONITOR@";

pub const CAPTURE_SAMPLE_RATE: u32 = 48_000;

pub const CAPTURE_CHANNELS: u8 = 1;

const READ_BYTES: usize = 1024 * 4;

const RING_CAPACITY: usize = 1024 * 128;

const CLIENT_NAME: &str = "Afterword";

const STREAM_NAME: &str = "system audio";

struct WakerState {
    waker: Option<Waker>,
    has_data: bool,
}

fn capture_spec() -> Spec {
    Spec {
        format: Format::F32le,
        channels: CAPTURE_CHANNELS,
        rate: CAPTURE_SAMPLE_RATE,
    }
}

fn capture_buffer_attr() -> BufferAttr {
    BufferAttr {
        maxlength: u32::MAX,
        fragsize: READ_BYTES as u32,
        ..Default::default()
    }
}

fn resolve_source_name(source: Option<&str>) -> String {
    match source {
        Some(s) if !s.trim().is_empty() => s.trim().to_string(),
        _ => DEFAULT_MONITOR_SOURCE.to_string(),
    }
}

pub fn source_for_device_name(device_name: &str) -> Option<String> {
    let trimmed = device_name
        .trim()
        .trim_end_matches("(System Audio)")
        .trim()
        .to_string();

    if trimmed.ends_with(".monitor") {
        Some(trimmed)
    } else {
        None
    }
}

fn bytes_to_f32(bytes: &[u8], out: &mut Vec<f32>) {
    out.clear();
    out.extend(
        bytes
            .chunks_exact(std::mem::size_of::<f32>())
            .map(|c| f32::from_le_bytes([c[0], c[1], c[2], c[3]])),
    );
}

pub struct PulseMonitorCapture {
    source: String,
}

impl PulseMonitorCapture {
    pub fn new(source: Option<&str>) -> Result<Self> {
        Ok(Self {
            source: resolve_source_name(source),
        })
    }

    pub fn source(&self) -> &str {
        &self.source
    }

    pub fn stream(self) -> Result<PulseMonitorStream> {
        let spec = capture_spec();
        if !spec.is_valid() {
            return Err(anyhow!(
                "Invalid PulseAudio sample spec ({} Hz, {} ch)",
                CAPTURE_SAMPLE_RATE,
                CAPTURE_CHANNELS
            ));
        }
        let attr = capture_buffer_attr();

        info!(
            "🔊 Pulse: connecting to source '{}' ({} Hz, {} ch, f32le)",
            self.source, CAPTURE_SAMPLE_RATE, CAPTURE_CHANNELS
        );

        let simple = Simple::new(
            None,
            CLIENT_NAME,
            Direction::Record,
            Some(self.source.as_str()),
            STREAM_NAME,
            &spec,
            None,
            Some(&attr),
        )
        .map_err(|e| {
            error!(
                "❌ Pulse: failed to connect to source '{}': {}",
                self.source, e
            );
            anyhow!(
                "Failed to open PulseAudio monitor source '{}': {}. \
                 PulseAudio/PipeWire may not be running, or libpulse is missing.",
                self.source,
                e
            )
        })?;

        info!("✅ Pulse: connected to source '{}'", self.source);

        let rb = HeapRb::<f32>::new(RING_CAPACITY);
        let (producer, consumer) = rb.split();

        let waker_state = Arc::new(Mutex::new(WakerState {
            waker: None,
            has_data: false,
        }));
        let should_terminate = Arc::new(AtomicBool::new(false));

        let thread = {
            let waker_state = waker_state.clone();
            let should_terminate = should_terminate.clone();
            let source = self.source.clone();
            std::thread::Builder::new()
                .name("afterword-pulse-monitor".to_string())
                .spawn(move || {
                    capture_loop(simple, producer, waker_state, should_terminate, source)
                })
                .map_err(|e| anyhow!("Failed to spawn PulseAudio capture thread: {}", e))?
        };

        Ok(PulseMonitorStream {
            consumer,
            waker_state,
            should_terminate,
            thread: Some(thread),
        })
    }
}

fn capture_loop(
    simple: Simple,
    mut producer: HeapProd<f32>,
    waker_state: Arc<Mutex<WakerState>>,
    should_terminate: Arc<AtomicBool>,
    source: String,
) {
    let mut bytes = vec![0u8; READ_BYTES];
    let mut samples: Vec<f32> = Vec::with_capacity(READ_BYTES / std::mem::size_of::<f32>());
    let mut consecutive_drops = 0u32;

    info!("✅ Pulse: capture thread started for '{}'", source);

    while !should_terminate.load(Ordering::Acquire) {
        if let Err(e) = simple.read(&mut bytes) {
            error!("❌ Pulse: read failed on '{}': {}", source, e);
            break;
        }

        bytes_to_f32(&bytes, &mut samples);
        let pushed = producer.push_slice(&samples);

        if pushed < samples.len() {
            consecutive_drops += 1;
            if consecutive_drops > 10 {
                warn!(
                    "⚠️ Pulse: consumer too slow, stopping capture for '{}'",
                    source
                );
                break;
            }
        } else {
            consecutive_drops = 0;
        }

        if pushed > 0 {
            wake_consumer(&waker_state);
        }
    }

    should_terminate.store(true, Ordering::Release);
    wake_consumer(&waker_state);
    info!("⚠️ Pulse: capture thread ended for '{}'", source);
}

fn wake_consumer(waker_state: &Arc<Mutex<WakerState>>) {
    let waker = {
        let mut state = match waker_state.lock() {
            Ok(state) => state,
            Err(poisoned) => poisoned.into_inner(),
        };
        if state.has_data {
            None
        } else {
            state.has_data = true;
            state.waker.take()
        }
    };
    if let Some(waker) = waker {
        waker.wake();
    }
}

pub struct PulseMonitorStream {
    consumer: HeapCons<f32>,
    waker_state: Arc<Mutex<WakerState>>,
    should_terminate: Arc<AtomicBool>,
    thread: Option<std::thread::JoinHandle<()>>,
}

impl PulseMonitorStream {
    pub fn sample_rate(&self) -> u32 {
        CAPTURE_SAMPLE_RATE
    }

    pub fn channels(&self) -> u16 {
        CAPTURE_CHANNELS as u16
    }
}

impl Stream for PulseMonitorStream {
    type Item = f32;

    fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        if let Some(sample) = self.consumer.try_pop() {
            return Poll::Ready(Some(sample));
        }

        if self.should_terminate.load(Ordering::Acquire) {
            return match self.consumer.try_pop() {
                Some(sample) => Poll::Ready(Some(sample)),
                None => Poll::Ready(None),
            };
        }

        {
            let mut state = match self.waker_state.lock() {
                Ok(state) => state,
                Err(poisoned) => poisoned.into_inner(),
            };
            state.has_data = false;
            state.waker = Some(cx.waker().clone());
        }

        if let Some(sample) = self.consumer.try_pop() {
            return Poll::Ready(Some(sample));
        }
        if self.should_terminate.load(Ordering::Acquire) {
            return Poll::Ready(None);
        }

        Poll::Pending
    }
}

impl Drop for PulseMonitorStream {
    fn drop(&mut self) {
        self.should_terminate.store(true, Ordering::Release);
        if let Some(thread) = self.thread.take() {
            if thread.join().is_err() {
                warn!("⚠️ Pulse: capture thread panicked");
            }
        }
        info!("PulseMonitorStream dropped");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_source_is_default_monitor() {
        assert_eq!(resolve_source_name(None), DEFAULT_MONITOR_SOURCE);
        assert_eq!(resolve_source_name(Some("   ")), DEFAULT_MONITOR_SOURCE);
        assert_eq!(
            resolve_source_name(Some("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor")),
            "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor"
        );
    }

    #[test]
    fn spec_is_valid_and_mono_48k() {
        let spec = capture_spec();
        assert!(spec.is_valid());
        assert_eq!(spec.rate, 48_000);
        assert_eq!(spec.channels, 1);
        assert_eq!(spec.format, Format::F32le);
    }

    #[test]
    fn buffer_attr_uses_fragsize_and_max_length() {
        let attr = capture_buffer_attr();
        assert_eq!(attr.maxlength, u32::MAX);
        assert_eq!(attr.fragsize, READ_BYTES as u32);

        assert_eq!(READ_BYTES, 4096);
        let samples_per_read = READ_BYTES / std::mem::size_of::<f32>();
        assert_eq!(samples_per_read, 1024);
        let fragment_ms = samples_per_read as u32 * 1000 / CAPTURE_SAMPLE_RATE;
        assert!(fragment_ms < 50, "fragment is {} ms", fragment_ms);
    }

    #[test]
    fn bytes_convert_to_samples() {
        let mut bytes = Vec::new();
        for v in [0.0f32, 1.0, -1.0, 0.5] {
            bytes.extend_from_slice(&v.to_le_bytes());
        }

        bytes.push(0x00);

        let mut out = Vec::new();
        bytes_to_f32(&bytes, &mut out);
        assert_eq!(out, vec![0.0, 1.0, -1.0, 0.5]);
    }

    #[test]
    fn device_names_map_to_sources() {
        assert_eq!(
            source_for_device_name("System Audio (PulseAudio/PipeWire)"),
            None
        );
        assert_eq!(
            source_for_device_name("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor"),
            Some("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor".to_string())
        );
        assert_eq!(
            source_for_device_name(
                "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor (System Audio)"
            ),
            Some("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor".to_string())
        );
        assert_eq!(source_for_device_name("Built-in Audio Analog Stereo"), None);
    }
}
