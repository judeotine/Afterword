use std::path::{Path, PathBuf};

use afterword_core::audio::constants::AUDIO_EXTENSIONS;
use afterword_core::audio::decoder::decode_audio_file;
use afterword_core::audio::vad::{get_speech_chunks, SpeechSegment};
use afterword_core::config::{DEFAULT_PARAKEET_MODEL, DEFAULT_WHISPER_MODEL};
use afterword_core::parakeet_engine::ParakeetEngine;
use afterword_core::transcript::{create_transcript_segments, write_transcripts_json};
use afterword_core::whisper_engine::WhisperEngine;
use clap::{Parser, ValueEnum};
use log::{debug, info, warn};

const VAD_REDEMPTION_TIME_MS: u32 = 2000;

const MAX_SEGMENT_SAMPLES: usize = 25 * 16000;

const MIN_SEGMENT_SAMPLES: usize = 1600;

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
enum EngineKind {
    Whisper,
    Parakeet,
}

impl EngineKind {
    fn as_str(self) -> &'static str {
        match self {
            EngineKind::Whisper => "whisper",
            EngineKind::Parakeet => "parakeet",
        }
    }

    fn default_model(self) -> &'static str {
        match self {
            EngineKind::Whisper => DEFAULT_WHISPER_MODEL,
            EngineKind::Parakeet => DEFAULT_PARAKEET_MODEL,
        }
    }
}

#[derive(Debug, Parser)]
#[command(
    name = "afterword-transcribe",
    version,
    about = "Transcribe an audio file with the Afterword pipeline (Whisper or Parakeet).",
    long_about = "Transcribe an audio file with the Afterword pipeline (Whisper or Parakeet).

Decodes the input to 16 kHz mono, segments it with Silero VAD, transcribes each
speech segment locally, and writes transcripts.json plus metadata.json into the
output directory. A one-line JSON summary is printed to stdout; logs go to stderr
(control them with RUST_LOG).

Models are never downloaded: provide a models directory that already contains
the requested model (whisper models sit flat in it, parakeet models under
<models-dir>/parakeet/<model>/).

Exit codes:
  0  success
  1  usage or other error
  2  decode failure
  3  requested model is not present locally"
)]
struct Cli {
    #[arg(long)]
    input: PathBuf,

    #[arg(long)]
    out: PathBuf,

    #[arg(long, value_enum, default_value_t = EngineKind::Whisper)]
    engine: EngineKind,

    #[arg(long)]
    model: Option<String>,

    #[arg(long = "models-dir")]
    models_dir: Option<PathBuf>,

    #[arg(long, default_value = "auto")]
    language: String,

    #[arg(long)]
    title: Option<String>,
}

impl Cli {
    fn resolved_model(&self) -> String {
        self.model
            .clone()
            .unwrap_or_else(|| self.engine.default_model().to_string())
    }

    fn resolved_title(&self) -> String {
        if let Some(title) = self.title.as_ref().map(|t| t.trim()) {
            if !title.is_empty() {
                return title.to_string();
            }
        }
        self.input
            .file_stem()
            .and_then(|s| s.to_str())
            .filter(|s| !s.is_empty())
            .unwrap_or("Bot recording")
            .to_string()
    }
}

#[derive(Debug)]
enum CliError {
    Usage(String),

    Decode(String),

    ModelMissing(String),
}

impl CliError {
    fn exit_code(&self) -> i32 {
        match self {
            CliError::Usage(_) => 1,
            CliError::Decode(_) => 2,
            CliError::ModelMissing(_) => 3,
        }
    }
}

impl std::fmt::Display for CliError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CliError::Usage(m) | CliError::Decode(m) | CliError::ModelMissing(m) => {
                write!(f, "{m}")
            }
        }
    }
}

fn resolve_models_dir(
    flag: Option<&Path>,
    env_value: Option<&str>,
    data_dir: Option<PathBuf>,
) -> Result<PathBuf, CliError> {
    if let Some(dir) = flag {
        return Ok(dir.to_path_buf());
    }
    if let Some(dir) = env_value.map(str::trim).filter(|v| !v.is_empty()) {
        return Ok(PathBuf::from(dir));
    }
    data_dir
        .map(|d| d.join("Afterword").join("models"))
        .ok_or_else(|| {
            CliError::Usage(
                "Could not determine a models directory; pass --models-dir or set AFTERWORD_MODELS_DIR"
                    .to_string(),
            )
        })
}

fn whisper_language_arg(language: &str) -> Option<String> {
    match language.trim() {
        "" | "auto" => None,
        other => Some(other.to_string()),
    }
}

fn validate_input(path: &Path) -> Result<(), CliError> {
    if !path.exists() {
        return Err(CliError::Usage(format!(
            "File does not exist: {}",
            path.display()
        )));
    }
    if !path.is_file() {
        return Err(CliError::Usage(format!("Not a file: {}", path.display())));
    }

    let extension = path
        .extension()
        .and_then(|e| e.to_str())
        .map(|e| e.to_lowercase())
        .unwrap_or_default();

    if !AUDIO_EXTENSIONS.contains(&extension.as_str()) {
        return Err(CliError::Usage(format!(
            "Unsupported format: .{}. Supported: {}",
            extension,
            AUDIO_EXTENSIONS.join(", ")
        )));
    }
    Ok(())
}

fn prepare_segments(segments: &[SpeechSegment]) -> Vec<SpeechSegment> {
    let mut processable = Vec::with_capacity(segments.len());
    for segment in segments {
        if segment.samples.len() > MAX_SEGMENT_SAMPLES {
            debug!(
                "Splitting large segment ({:.0}ms, {} samples) at silence boundaries",
                segment.end_timestamp_ms - segment.start_timestamp_ms,
                segment.samples.len()
            );
            processable.extend(afterword_core::transcript::split_segment_at_silence(
                segment,
                MAX_SEGMENT_SAMPLES,
            ));
        } else {
            processable.push(segment.clone());
        }
    }
    processable
}

fn model_missing_message(engine: EngineKind, model: &str, expected_path: &Path) -> String {
    format!(
        "{} model '{}' is not available at {}. afterword-transcribe never downloads models — \
download it once with the Afterword desktop app (Settings → Transcription → Models, \
which runs the `{}_download_model` command) and point --models-dir at that directory.",
        engine.as_str(),
        model,
        expected_path.display(),
        engine.as_str()
    )
}

enum LoadedEngine {
    Whisper(Box<WhisperEngine>),
    Parakeet(Box<ParakeetEngine>),
}

async fn prepare_engine(
    engine: EngineKind,
    models_dir: &Path,
    model: &str,
) -> Result<LoadedEngine, CliError> {
    match engine {
        EngineKind::Whisper => {
            let whisper = WhisperEngine::new_with_models_dir(Some(models_dir.to_path_buf()))
                .map_err(|e| CliError::Usage(format!("Failed to create Whisper engine: {e}")))?;
            let models = whisper
                .discover_models()
                .await
                .map_err(|e| CliError::Usage(format!("Model discovery failed: {e}")))?;
            let info = models.iter().find(|m| m.name == model).ok_or_else(|| {
                CliError::Usage(format!(
                    "Unknown whisper model '{}'. Known models: {}",
                    model,
                    models
                        .iter()
                        .map(|m| m.name.as_str())
                        .collect::<Vec<_>>()
                        .join(", ")
                ))
            })?;
            if !matches!(
                info.status,
                afterword_core::whisper_engine::ModelStatus::Available
            ) {
                return Err(CliError::ModelMissing(model_missing_message(
                    engine, model, &info.path,
                )));
            }
            Ok(LoadedEngine::Whisper(Box::new(whisper)))
        }
        EngineKind::Parakeet => {
            let parakeet = ParakeetEngine::new_with_models_dir(Some(models_dir.to_path_buf()))
                .map_err(|e| CliError::Usage(format!("Failed to create Parakeet engine: {e}")))?;
            let models = parakeet
                .discover_models()
                .await
                .map_err(|e| CliError::Usage(format!("Model discovery failed: {e}")))?;
            let info = models.iter().find(|m| m.name == model).ok_or_else(|| {
                CliError::Usage(format!(
                    "Unknown parakeet model '{}'. Known models: {}",
                    model,
                    models
                        .iter()
                        .map(|m| m.name.as_str())
                        .collect::<Vec<_>>()
                        .join(", ")
                ))
            })?;
            if !matches!(
                info.status,
                afterword_core::parakeet_engine::ModelStatus::Available
            ) {
                return Err(CliError::ModelMissing(model_missing_message(
                    engine, model, &info.path,
                )));
            }
            Ok(LoadedEngine::Parakeet(Box::new(parakeet)))
        }
    }
}

impl LoadedEngine {
    async fn load(&self, model: &str) -> Result<(), CliError> {
        let result = match self {
            LoadedEngine::Whisper(e) => e.load_model(model).await,
            LoadedEngine::Parakeet(e) => e.load_model(model).await,
        };
        result.map_err(|e| CliError::Usage(format!("Failed to load model '{model}': {e}")))
    }

    async fn transcribe(
        &self,
        samples: Vec<f32>,
        language: Option<String>,
    ) -> Result<String, CliError> {
        match self {
            LoadedEngine::Whisper(e) => e
                .transcribe_audio_with_confidence(samples, language)
                .await
                .map(|(text, _confidence, _)| text)
                .map_err(|e| CliError::Usage(format!("Whisper transcription failed: {e}"))),
            LoadedEngine::Parakeet(e) => e
                .transcribe_audio(samples)
                .await
                .map_err(|e| CliError::Usage(format!("Parakeet transcription failed: {e}"))),
        }
    }
}

#[allow(clippy::too_many_arguments)]
fn write_metadata_json(
    out_dir: &Path,
    id: &str,
    title: &str,
    duration_seconds: f64,
    engine: EngineKind,
    model: &str,
    language: &str,
    segments_count: usize,
) -> Result<PathBuf, CliError> {
    let metadata_path = out_dir.join("metadata.json");
    let temp_path = out_dir.join(".metadata.json.tmp");

    let json = serde_json::json!({
        "id": id,
        "title": title,
        "source": "bot",
        "duration_seconds": duration_seconds,
        "engine": engine.as_str(),
        "model": model,
        "language": language,
        "created_at": chrono::Utc::now().to_rfc3339(),
        "segments_count": segments_count,
    });

    let json_string = serde_json::to_string_pretty(&json)
        .map_err(|e| CliError::Usage(format!("Failed to serialize metadata.json: {e}")))?;
    std::fs::write(&temp_path, &json_string)
        .map_err(|e| CliError::Usage(format!("Failed to write metadata.json: {e}")))?;
    std::fs::rename(&temp_path, &metadata_path)
        .map_err(|e| CliError::Usage(format!("Failed to write metadata.json: {e}")))?;

    Ok(metadata_path)
}

async fn run(cli: Cli) -> Result<String, CliError> {
    validate_input(&cli.input)?;

    let models_dir = resolve_models_dir(
        cli.models_dir.as_deref(),
        std::env::var("AFTERWORD_MODELS_DIR").ok().as_deref(),
        dirs::data_dir().or_else(dirs::home_dir),
    )?;
    let model = cli.resolved_model();
    let title = cli.resolved_title();
    info!(
        "Transcribing {} with {} model '{}' from {}",
        cli.input.display(),
        cli.engine.as_str(),
        model,
        models_dir.display()
    );

    let engine = prepare_engine(cli.engine, &models_dir, &model).await?;

    std::fs::create_dir_all(&cli.out)
        .map_err(|e| CliError::Usage(format!("Failed to create {}: {e}", cli.out.display())))?;

    let input_path = cli.input.clone();
    let decoded = tokio::task::spawn_blocking(move || decode_audio_file(&input_path))
        .await
        .map_err(|e| CliError::Usage(format!("Decode task panicked: {e}")))?
        .map_err(|e| CliError::Decode(format!("Failed to decode {}: {e}", cli.input.display())))?;
    let duration_seconds = decoded.duration_seconds;
    info!(
        "Decoded audio: {:.2}s, {}Hz, {} channels",
        duration_seconds, decoded.sample_rate, decoded.channels
    );

    let audio_samples = tokio::task::spawn_blocking(move || decoded.to_whisper_format())
        .await
        .map_err(|e| CliError::Usage(format!("Resample task panicked: {e}")))?;

    let speech_segments = tokio::task::spawn_blocking(move || {
        get_speech_chunks(&audio_samples, VAD_REDEMPTION_TIME_MS)
    })
    .await
    .map_err(|e| CliError::Usage(format!("VAD task panicked: {e}")))?
    .map_err(|e| CliError::Usage(format!("VAD processing failed: {e}")))?;
    info!(
        "VAD detected {} speech segments (redemption_time={}ms)",
        speech_segments.len(),
        VAD_REDEMPTION_TIME_MS
    );
    if speech_segments.is_empty() {
        warn!("No speech detected in {}", cli.input.display());
    }

    let processable = prepare_segments(&speech_segments);
    let processable_count = processable.len();
    info!("Processing {processable_count} segments (after splitting)");

    let language = whisper_language_arg(&cli.language);
    let mut all_transcripts: Vec<(String, f64, f64)> = Vec::new();
    if processable_count > 0 {
        engine.load(&model).await?;
    }
    for (i, segment) in processable.iter().enumerate() {
        if segment.samples.len() < MIN_SEGMENT_SAMPLES {
            debug!(
                "Skipping short segment {i} with {} samples",
                segment.samples.len()
            );
            continue;
        }
        let text = engine
            .transcribe(segment.samples.clone(), language.clone())
            .await?;
        let trimmed = text.trim();
        if trimmed.is_empty() {
            debug!("Segment {}/{processable_count}: empty transcription", i + 1);
            continue;
        }
        debug!(
            "Segment {}/{processable_count}: {:.1}s transcribed",
            i + 1,
            (segment.end_timestamp_ms - segment.start_timestamp_ms) / 1000.0
        );
        all_transcripts.push((text, segment.start_timestamp_ms, segment.end_timestamp_ms));
    }
    info!(
        "Transcription complete: {} of {} segments produced text",
        all_transcripts.len(),
        processable_count
    );

    let segments = create_transcript_segments(&all_transcripts);
    write_transcripts_json(&cli.out, &segments)
        .map_err(|e| CliError::Usage(format!("Failed to write transcripts.json: {e}")))?;
    let transcript_path = cli.out.join("transcripts.json");

    let metadata_path = write_metadata_json(
        &cli.out,
        &format!("bot-{}", uuid::Uuid::new_v4()),
        &title,
        duration_seconds,
        cli.engine,
        &model,
        &cli.language,
        segments.len(),
    )?;

    let summary = serde_json::json!({
        "transcript": transcript_path.to_string_lossy(),
        "metadata": metadata_path.to_string_lossy(),
        "segments": segments.len(),
        "duration_seconds": duration_seconds,
    });
    Ok(summary.to_string())
}

#[tokio::main]
async fn main() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info"))
        .target(env_logger::Target::Stderr)
        .init();

    let cli = Cli::parse();
    match run(cli).await {
        Ok(summary) => println!("{summary}"),
        Err(err) => {
            eprintln!("afterword-transcribe: {err}");
            std::process::exit(err.exit_code());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(args: &[&str]) -> Cli {
        Cli::parse_from(args)
    }

    #[test]
    fn defaults_are_whisper_auto_and_engine_default_model() {
        let cli = parse(&[
            "afterword-transcribe",
            "--input",
            "/tmp/meeting.wav",
            "--out",
            "/tmp/out",
        ]);
        assert_eq!(cli.engine, EngineKind::Whisper);
        assert_eq!(cli.language, "auto");
        assert_eq!(cli.model, None);
        assert_eq!(cli.models_dir, None);
        assert_eq!(cli.resolved_model(), DEFAULT_WHISPER_MODEL);
        assert_eq!(cli.resolved_title(), "meeting");
    }

    #[test]
    fn parakeet_engine_defaults_to_the_parakeet_model() {
        let cli = parse(&[
            "afterword-transcribe",
            "--input",
            "/tmp/meeting.wav",
            "--out",
            "/tmp/out",
            "--engine",
            "parakeet",
        ]);
        assert_eq!(cli.engine, EngineKind::Parakeet);
        assert_eq!(cli.resolved_model(), DEFAULT_PARAKEET_MODEL);
    }

    #[test]
    fn explicit_flags_win_over_defaults() {
        let cli = parse(&[
            "afterword-transcribe",
            "--input",
            "/tmp/meeting.m4a",
            "--out",
            "/tmp/out",
            "--engine",
            "whisper",
            "--model",
            "tiny",
            "--models-dir",
            "/models",
            "--language",
            "auto-translate",
            "--title",
            "Standup",
        ]);
        assert_eq!(cli.resolved_model(), "tiny");
        assert_eq!(cli.models_dir.as_deref(), Some(Path::new("/models")));
        assert_eq!(cli.language, "auto-translate");
        assert_eq!(cli.resolved_title(), "Standup");
    }

    #[test]
    fn blank_title_falls_back_to_the_input_stem() {
        let cli = parse(&[
            "afterword-transcribe",
            "--input",
            "/tmp/weekly sync.wav",
            "--out",
            "/tmp/out",
            "--title",
            "   ",
        ]);
        assert_eq!(cli.resolved_title(), "weekly sync");
    }

    #[test]
    fn exit_codes_match_the_documented_contract() {
        assert_eq!(CliError::Usage("x".into()).exit_code(), 1);
        assert_eq!(CliError::Decode("x".into()).exit_code(), 2);
        assert_eq!(CliError::ModelMissing("x".into()).exit_code(), 3);
    }

    #[test]
    fn models_dir_prefers_flag_then_env_then_data_dir() {
        let from_flag = resolve_models_dir(
            Some(Path::new("/flag")),
            Some("/env"),
            Some(PathBuf::from("/data")),
        )
        .unwrap();
        assert_eq!(from_flag, PathBuf::from("/flag"));

        let from_env =
            resolve_models_dir(None, Some("/env"), Some(PathBuf::from("/data"))).unwrap();
        assert_eq!(from_env, PathBuf::from("/env"));

        let from_data = resolve_models_dir(None, None, Some(PathBuf::from("/data"))).unwrap();
        assert_eq!(from_data, PathBuf::from("/data/Afterword/models"));

        let blank_env = resolve_models_dir(None, Some("  "), Some(PathBuf::from("/data"))).unwrap();
        assert_eq!(blank_env, PathBuf::from("/data/Afterword/models"));
    }

    #[test]
    fn models_dir_without_any_source_is_a_usage_error() {
        let err = resolve_models_dir(None, None, None).unwrap_err();
        assert_eq!(err.exit_code(), 1);
    }

    #[test]
    fn language_auto_maps_to_none_and_others_pass_through() {
        assert_eq!(whisper_language_arg("auto"), None);
        assert_eq!(whisper_language_arg(""), None);
        assert_eq!(
            whisper_language_arg("auto-translate"),
            Some("auto-translate".to_string())
        );
        assert_eq!(whisper_language_arg("es"), Some("es".to_string()));
    }

    #[test]
    fn missing_input_and_bad_extension_are_usage_errors() {
        let dir = tempfile::tempdir().unwrap();

        let missing = dir.path().join("nope.wav");
        let err = validate_input(&missing).unwrap_err();
        assert_eq!(err.exit_code(), 1);
        assert!(err.to_string().contains("does not exist"));

        let text = dir.path().join("notes.txt");
        std::fs::write(&text, b"x").unwrap();
        let err = validate_input(&text).unwrap_err();
        assert_eq!(err.exit_code(), 1);
        assert!(err.to_string().contains("Unsupported format"));

        let wav = dir.path().join("ok.WAV");
        std::fs::write(&wav, b"x").unwrap();
        assert!(validate_input(&wav).is_ok());
    }

    #[test]
    fn model_missing_message_names_path_and_download_route() {
        let msg = model_missing_message(
            EngineKind::Whisper,
            "tiny",
            Path::new("/models/ggml-tiny.bin"),
        );
        assert!(msg.contains("/models/ggml-tiny.bin"));
        assert!(msg.contains("Afterword desktop app"));
        assert!(msg.contains("never downloads models"));
    }

    #[test]
    fn prepare_segments_splits_only_oversized_segments() {
        let short = SpeechSegment {
            samples: vec![0.0; 16000],
            start_timestamp_ms: 0.0,
            end_timestamp_ms: 1000.0,
            confidence: 0.9,
        };
        assert_eq!(prepare_segments(std::slice::from_ref(&short)).len(), 1);

        let long = SpeechSegment {
            samples: vec![0.1; MAX_SEGMENT_SAMPLES * 2 + 16000],
            start_timestamp_ms: 0.0,
            end_timestamp_ms: 51000.0,
            confidence: 0.9,
        };
        assert!(prepare_segments(&[long]).len() > 1);
    }
}
