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
