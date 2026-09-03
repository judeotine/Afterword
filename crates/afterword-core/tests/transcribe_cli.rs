//! End-to-end tests for the `afterword-transcribe` binary.
//!
//! The default test runs offline: it feeds the CLI a generated silent WAV and an
//! empty models directory and asserts the documented "model missing" exit code.
//! The real-transcription test is `#[ignore]`d and only runs when
//! `AFTERWORD_TEST_MODELS_DIR` points at a directory with a downloaded model.

use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::Command;

/// Exit code the CLI documents for "requested model is not present locally".
const EXIT_MODEL_MISSING: i32 = 3;

fn binary() -> PathBuf {
    PathBuf::from(env!("CARGO_BIN_EXE_afterword-transcribe"))
}

/// Write a mono 16-bit PCM WAV of `seconds` of digital silence at 16 kHz.
///
/// Symphonia decodes, it does not encode, so the header is written by hand.
fn write_silent_wav(path: &Path, seconds: u32) {
    const SAMPLE_RATE: u32 = 16_000;
    const CHANNELS: u16 = 1;
    const BITS_PER_SAMPLE: u16 = 16;

    let frames = SAMPLE_RATE * seconds;
    let byte_rate = SAMPLE_RATE * CHANNELS as u32 * (BITS_PER_SAMPLE / 8) as u32;
    let block_align = CHANNELS * (BITS_PER_SAMPLE / 8);
    let data_len = frames * block_align as u32;

    let mut bytes: Vec<u8> = Vec::with_capacity(44 + data_len as usize);
    bytes.extend_from_slice(b"RIFF");
    bytes.extend_from_slice(&(36 + data_len).to_le_bytes());
    bytes.extend_from_slice(b"WAVE");
    bytes.extend_from_slice(b"fmt ");
    bytes.extend_from_slice(&16u32.to_le_bytes()); // PCM fmt chunk size
    bytes.extend_from_slice(&1u16.to_le_bytes()); // PCM format tag
    bytes.extend_from_slice(&CHANNELS.to_le_bytes());
    bytes.extend_from_slice(&SAMPLE_RATE.to_le_bytes());
    bytes.extend_from_slice(&byte_rate.to_le_bytes());
    bytes.extend_from_slice(&block_align.to_le_bytes());
    bytes.extend_from_slice(&BITS_PER_SAMPLE.to_le_bytes());
    bytes.extend_from_slice(b"data");
    bytes.extend_from_slice(&data_len.to_le_bytes());
    bytes.resize(44 + data_len as usize, 0);

    let mut file = std::fs::File::create(path).expect("create wav");
    file.write_all(&bytes).expect("write wav");
    file.flush().expect("flush wav");
}

#[test]
fn missing_whisper_model_exits_with_code_3() {
    let dir = tempfile::tempdir().expect("tempdir");
    let wav = dir.path().join("silence.wav");
    write_silent_wav(&wav, 3);

    let models_dir = dir.path().join("models");
    std::fs::create_dir_all(&models_dir).expect("models dir");
    let out_dir = dir.path().join("out");

    let output = Command::new(binary())
        .args([
            "--input",
            wav.to_str().unwrap(),
            "--out",
            out_dir.to_str().unwrap(),
            "--engine",
            "whisper",
            "--model",
            "tiny",
            "--models-dir",
            models_dir.to_str().unwrap(),
        ])
        .output()
        .expect("run afterword-transcribe");

    let stderr = String::from_utf8_lossy(&output.stderr);
    assert_eq!(
        output.status.code(),
        Some(EXIT_MODEL_MISSING),
        "expected exit code {EXIT_MODEL_MISSING}, stderr was:\n{stderr}"
    );
    assert!(
        stderr.contains("ggml-tiny.bin"),
        "error should name the expected model path, stderr was:\n{stderr}"
    );
    assert!(
        stderr.contains("Afterword desktop app"),
        "error should point at the desktop app's download flow, stderr was:\n{stderr}"
    );
}

#[test]
fn unsupported_extension_exits_with_code_1() {
    let dir = tempfile::tempdir().expect("tempdir");
    let input = dir.path().join("notes.txt");
    std::fs::write(&input, b"not audio").expect("write input");

    let output = Command::new(binary())
        .args([
            "--input",
            input.to_str().unwrap(),
            "--out",
            dir.path().join("out").to_str().unwrap(),
            "--models-dir",
            dir.path().to_str().unwrap(),
        ])
        .output()
        .expect("run afterword-transcribe");

    assert_eq!(output.status.code(), Some(1));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("Unsupported format"),
        "stderr was:\n{stderr}"
    );
}

/// Real transcription against a downloaded model.
///
/// Run with: `AFTERWORD_TEST_MODELS_DIR=/path/to/models cargo test -p afterword-core
/// --test transcribe_cli -- --ignored`
#[test]
#[ignore = "requires AFTERWORD_TEST_MODELS_DIR with a downloaded model"]
fn transcribes_with_a_real_model() {
    let models_dir = match std::env::var("AFTERWORD_TEST_MODELS_DIR") {
        Ok(v) if !v.is_empty() => PathBuf::from(v),
        _ => panic!("AFTERWORD_TEST_MODELS_DIR must be set for this test"),
    };
    let model = std::env::var("AFTERWORD_TEST_MODEL").unwrap_or_else(|_| "tiny".to_string());

    let dir = tempfile::tempdir().expect("tempdir");
    let wav = dir.path().join("silence.wav");
    write_silent_wav(&wav, 3);
    let out_dir = dir.path().join("out");

    let output = Command::new(binary())
        .args([
            "--input",
            wav.to_str().unwrap(),
            "--out",
            out_dir.to_str().unwrap(),
            "--engine",
            "whisper",
            "--model",
            &model,
            "--models-dir",
            models_dir.to_str().unwrap(),
        ])
        .output()
        .expect("run afterword-transcribe");

    let stderr = String::from_utf8_lossy(&output.stderr);
    assert_eq!(output.status.code(), Some(0), "stderr was:\n{stderr}");

    let stdout = String::from_utf8_lossy(&output.stdout);
    let summary: serde_json::Value =
        serde_json::from_str(stdout.trim()).expect("stdout is a one-line JSON summary");
    assert!(summary["transcript"].is_string());
    assert!(summary["metadata"].is_string());
    assert!(summary["segments"].is_number());
    assert!(summary["duration_seconds"].is_number());

    assert!(out_dir.join("transcripts.json").exists());
    assert!(out_dir.join("metadata.json").exists());
}
