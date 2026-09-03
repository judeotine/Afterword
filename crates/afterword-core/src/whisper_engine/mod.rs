pub mod acceleration;
// The file layout (whisper_engine/whisper_engine.rs) is what the desktop app's
// re-exports import from; renaming it would churn every call site.
#[allow(clippy::module_inception)]
pub mod whisper_engine;

pub use acceleration::*;
pub use whisper_engine::*;
