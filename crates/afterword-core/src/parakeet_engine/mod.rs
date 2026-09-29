pub mod model;

#[allow(clippy::module_inception)]
pub mod parakeet_engine;

pub use model::{ParakeetError, ParakeetModel, TimestampedResult};
pub use parakeet_engine::{
    DownloadProgress, ModelInfo, ModelStatus, ParakeetEngine, ParakeetEngineError, QuantizationType,
};
