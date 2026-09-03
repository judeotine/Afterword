// Linux system-audio capture via the PulseAudio / PipeWire default monitor source.
//
// PulseAudio (and PipeWire's pulse server) exposes a "monitor" source for every
// sink. Recording from the monitor of the default sink gives us exactly what the
// user hears, which is the Linux equivalent of the macOS Core Audio tap in
// `capture/core_audio.rs`. This module mirrors that file's shape: a background
// producer fills a lock-free ring buffer and `PulseMonitorStream` implements
// `futures_util::Stream<Item = f32>` on the consumer side.
//
// The producer here is a plain `std::thread` (not a callback) because
// `psimple::Simple::read` is a blocking call.

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

/// Source name that PulseAudio/PipeWire resolves to the monitor of the default sink.
pub const DEFAULT_MONITOR_SOURCE: &str = "@DEFAULT_MONITOR@";

/// Capture sample rate. The pipeline resamples to its own target if needed.
pub const CAPTURE_SAMPLE_RATE: u32 = 48_000;

/// Capture channel count. Mono, matching the macOS Core Audio tap.
pub const CAPTURE_CHANNELS: u8 = 1;

/// Bytes requested per blocking `read()` (1024 f32 samples ≈ 21 ms at 48 kHz).
///
/// This also bounds how long `PulseMonitorStream::drop` can block while joining
/// the capture thread, so keep it comfortably under the 50 ms settle that
/// `AudioStream::stop` allows after aborting the polling task.
const READ_BYTES: usize = 1024 * 4;

/// Ring buffer capacity in samples (same as the Core Audio path).
const RING_CAPACITY: usize = 1024 * 128;

/// Client name reported to the sound server.
const CLIENT_NAME: &str = "Afterword";

/// Stream name reported to the sound server.
const STREAM_NAME: &str = "system audio";

/// Waker state shared between the capture thread and the async consumer.
struct WakerState {
    waker: Option<Waker>,
    has_data: bool,
}

/// The capture spec handed to PulseAudio: mono 32-bit float, little endian.
fn capture_spec() -> Spec {
    Spec {
        format: Format::F32le,
        channels: CAPTURE_CHANNELS,
        rate: CAPTURE_SAMPLE_RATE,
    }
}

/// Buffering attributes: only `maxlength` and `fragsize` matter for record streams.
fn capture_buffer_attr() -> BufferAttr {
    BufferAttr {
        maxlength: u32::MAX,
        fragsize: READ_BYTES as u32,
        ..Default::default()
    }
}

/// Resolve the PulseAudio source name to record from.
///
/// `None` (or an empty/whitespace name) means "monitor of the default sink".
fn resolve_source_name(source: Option<&str>) -> String {
    match source {
        Some(s) if !s.trim().is_empty() => s.trim().to_string(),
        _ => DEFAULT_MONITOR_SOURCE.to_string(),
    }
}

/// Map a UI device name to a PulseAudio source name.
///
/// The synthetic "System Audio (PulseAudio/PipeWire)" entry, and anything we do
/// not recognise, map to `None` (the default monitor). A name that looks like a
/// real Pulse monitor source (`...monitor`, optionally with the " (System Audio)"
/// suffix the ALSA scan appends) is used verbatim.
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

/// Convert a byte buffer of little-endian f32 samples into `f32`s.
///
/// Any trailing partial sample is ignored (`read()` always fills whole frames,
/// but this keeps the conversion total).
fn bytes_to_f32(bytes: &[u8], out: &mut Vec<f32>) {
    out.clear();
    out.extend(
        bytes
            .chunks_exact(std::mem::size_of::<f32>())
            .map(|c| f32::from_le_bytes([c[0], c[1], c[2], c[3]])),
    );
}

/// System audio capture backed by a PulseAudio/PipeWire monitor source.
pub struct PulseMonitorCapture {
    source: String,
}

impl PulseMonitorCapture {
    /// Create a capture handle for `source`, defaulting to the default monitor.
    ///
    /// This does not talk to the sound server yet; [`Self::stream`] connects.
    pub fn new(source: Option<&str>) -> Result<Self> {
        Ok(Self {
            source: resolve_source_name(source),
        })
    }

    /// The source name this capture will record from.
    pub fn source(&self) -> &str {
        &self.source
    }

    /// Connect to the sound server and start producing samples.
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

        // Connect on the calling thread so failures surface synchronously.
        // `Simple` is Send, so it is then moved into the capture thread.
        let simple = Simple::new(
            None, // default server
            CLIENT_NAME,
            Direction::Record,
            Some(self.source.as_str()),
            STREAM_NAME,
            &spec,
            None, // default channel map
            Some(&attr),
        )
        .map_err(|e| {
            error!("❌ Pulse: failed to connect to source '{}': {}", self.source, e);
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

/// Blocking read loop, owns the `Simple` connection for its whole lifetime.
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
                warn!("⚠️ Pulse: consumer too slow, stopping capture for '{}'", source);
                break;
            }
        } else {
            consecutive_drops = 0;
        }

        if pushed > 0 {
            wake_consumer(&waker_state);
        }
    }

    // Tell the consumer no more samples are coming and wake it so it observes that.
    should_terminate.store(true, Ordering::Release);
    wake_consumer(&waker_state);
    info!("⚠️ Pulse: capture thread ended for '{}'", source);
    // `simple` is dropped here, closing the connection from the thread that owns it.
}

fn wake_consumer(waker_state: &Arc<Mutex<WakerState>>) {
    // Only wake on the transition from "drained" to "has data": the consumer
    // clears `has_data` when it finds the ring empty, so a wake already pending
    // does not need another one.
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

/// Async stream of mono f32 samples from a PulseAudio/PipeWire monitor source.
pub struct PulseMonitorStream {
    consumer: HeapCons<f32>,
    waker_state: Arc<Mutex<WakerState>>,
    should_terminate: Arc<AtomicBool>,
    thread: Option<std::thread::JoinHandle<()>>,
}

impl PulseMonitorStream {
    /// Sample rate of the samples produced by this stream.
    pub fn sample_rate(&self) -> u32 {
        CAPTURE_SAMPLE_RATE
    }

    /// Channel count of the samples produced by this stream.
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
            // Drain anything that landed between the two checks, then end.
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

        // The capture thread may have pushed or terminated between the checks
        // above and the waker being stored; re-check so we never park on a
        // stream that will produce no further wake-ups.
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
    /// Stops the capture thread and waits for it, so `Simple` is always freed by
    /// the thread that is blocked inside `read()` on it.
    ///
    /// The join is bounded by one fragment (`READ_BYTES`, ≈ 21 ms) as long as the
    /// monitor keeps producing, which it does whenever the sink is running. A
    /// fully stalled monitor (e.g. a sink suspended by `module-suspend-on-idle`)
    /// would block the caller — on Linux that caller is the tokio worker running
    /// the aborted polling task. If that is ever observed, the structural fix is
    /// to signal and detach here, and join the capture thread from a dedicated
    /// std::thread reaper instead of from the runtime.
    fn drop(&mut self) {
        self.should_terminate.store(true, Ordering::Release);
        if let Some(thread) = self.thread.take() {
            // The capture thread checks the flag after each blocking read, so this
            // returns within roughly one fragment (~85 ms).
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

        // One fragment is 1024 mono f32 samples ≈ 21 ms at 48 kHz, which bounds
        // the join in `PulseMonitorStream::drop`; it must stay under the 50 ms
        // settle `AudioStream::stop` allows.
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
        // A trailing partial sample must be ignored, not panic.
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
            source_for_device_name("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor (System Audio)"),
            Some("alsa_output.pci-0000_00_1f.3.analog-stereo.monitor".to_string())
        );
        assert_eq!(source_for_device_name("Built-in Audio Analog Stereo"), None);
    }
}
