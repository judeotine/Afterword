pub mod commands;

// Engine and acceleration detection live in afterword-core; re-exported here
// so `crate::whisper_engine::...` paths keep working unchanged.
pub use afterword_core::whisper_engine::*;
pub use commands::*;
