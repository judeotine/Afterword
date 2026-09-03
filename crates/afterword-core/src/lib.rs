//! Afterword core: the Tauri-free part of the transcription pipeline.
//!
//! This crate holds the audio decoding/processing, VAD, and speech-to-text
//! engines (Whisper and Parakeet) so they can be shared by the desktop app
//! (`app_lib`), a CLI, and a server-side transcription worker.

// Performance optimization: Conditional logging macros for hot paths.
// Defined before the module declarations so they are textually in scope for
// every module in this crate (same as they were in app_lib's lib.rs).
#[cfg(debug_assertions)]
#[macro_export]
macro_rules! perf_debug {
    ($($arg:tt)*) => {
        log::debug!($($arg)*)
    };
}

#[cfg(not(debug_assertions))]
#[macro_export]
macro_rules! perf_debug {
    ($($arg:tt)*) => {};
}

#[cfg(debug_assertions)]
#[macro_export]
macro_rules! perf_trace {
    ($($arg:tt)*) => {
        log::trace!($($arg)*)
    };
}

#[cfg(not(debug_assertions))]
#[macro_export]
macro_rules! perf_trace {
    ($($arg:tt)*) => {};
}

pub mod audio;
pub mod config;
pub mod parakeet_engine;
pub mod transcript;
pub mod whisper_engine;
