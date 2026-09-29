use std::fs;
use std::path::PathBuf;
use std::process::exit;

use afterword_core::summary::{
    summarize_transcript, OfflineProvider, OllamaProvider, SummaryProvider, Template,
};
use afterword_core::transcript::ApiTranscriptSegment;
use clap::Parser;

#[derive(Parser)]
#[command(name = "afterword-summarise")]
#[command(about = "Summarise a transcript into a templated meeting report")]
struct Cli {
    #[arg(long)]
    transcript: PathBuf,
    #[arg(long)]
    template: PathBuf,
    #[arg(long)]
    out: PathBuf,
    #[arg(long, default_value = "offline")]
    provider: String,
    #[arg(long, default_value = "http://localhost:11434")]
    ollama_url: String,
    #[arg(long, default_value = "llama3")]
    model: String,
}

fn transcript_text(path: &PathBuf) -> Result<String, String> {
    let raw = fs::read_to_string(path).map_err(|e| format!("read transcript: {e}"))?;
    let segments: Vec<ApiTranscriptSegment> =
        serde_json::from_str(&raw).map_err(|e| format!("parse transcript json: {e}"))?;
    let mut text = String::new();
    for segment in segments {
        let trimmed = segment.text.trim();
        if trimmed.is_empty() {
            continue;
        }
        text.push_str(trimmed);
        text.push('\n');
    }
    Ok(text)
}

fn load_template(path: &PathBuf) -> Result<Template, String> {
    let raw = fs::read_to_string(path).map_err(|e| format!("read template: {e}"))?;
    let template: Template =
        serde_json::from_str(&raw).map_err(|e| format!("parse template json: {e}"))?;
    template.validate()?;
    Ok(template)
}

fn run() -> Result<(), String> {
    let cli = Cli::parse();
    let transcript = transcript_text(&cli.transcript)?;
    let template = load_template(&cli.template)?;

    let provider: Box<dyn SummaryProvider> = match cli.provider.as_str() {
        "offline" => Box::new(OfflineProvider),
        "ollama" => Box::new(OllamaProvider::new(
            cli.ollama_url.clone(),
            cli.model.clone(),
        )),
        other => return Err(format!("unknown provider '{other}'")),
    };

    let markdown = summarize_transcript(&template, &transcript, provider.as_ref())
        .map_err(|e| format!("summarise: {e}"))?;

    if let Some(parent) = cli.out.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("create out dir: {e}"))?;
    }
    fs::write(&cli.out, markdown.as_bytes()).map_err(|e| format!("write summary: {e}"))?;
    println!("{}", cli.out.to_string_lossy());
    Ok(())
}

fn main() {
    if let Err(err) = run() {
        eprintln!("afterword-summarise: {err}");
        exit(1);
    }
}
