use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};

pub struct SummaryRequest {
    pub system_prompt: String,
    pub transcript: String,
    pub template_markdown: String,
}

pub trait SummaryProvider {
    fn summarize(&self, request: &SummaryRequest) -> Result<String>;
    fn name(&self) -> &str;
}

pub struct OfflineProvider;

impl SummaryProvider for OfflineProvider {
    fn summarize(&self, request: &SummaryRequest) -> Result<String> {
        let mut out = String::new();
        for line in request.template_markdown.lines() {
            if let Some(section) = line.strip_prefix("**").and_then(|s| s.strip_suffix("**")) {
                out.push_str(&format!("## {}\n\n", section));
                out.push_str("Not available offline.\n\n");
            } else if let Some(title) = line.strip_prefix("# ") {
                out.push_str(&format!(
                    "# {}\n\n",
                    title.replace("<Add Title here>", "Meeting Summary")
                ));
            }
        }
        if out.is_empty() {
            out.push_str("# Meeting Summary\n\nNot available offline.\n");
        }
        Ok(out)
    }

    fn name(&self) -> &str {
        "offline"
    }
}

#[derive(Serialize)]
struct OllamaChatRequest {
    model: String,
    messages: Vec<OllamaMessage>,
    stream: bool,
}

#[derive(Serialize, Deserialize)]
struct OllamaMessage {
    role: String,
    content: String,
}

#[derive(Deserialize)]
struct OllamaChatResponse {
    message: OllamaMessage,
}

pub struct OllamaProvider {
    base_url: String,
    model: String,
    client: reqwest::blocking::Client,
}

impl OllamaProvider {
    pub fn new(base_url: String, model: String) -> Self {
        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            model,
            client: reqwest::blocking::Client::new(),
        }
    }
}

impl SummaryProvider for OllamaProvider {
    fn summarize(&self, request: &SummaryRequest) -> Result<String> {
        let user = format!(
            "<source_transcript>\n{}\n</source_transcript>",
            request.transcript
        );
        let body = OllamaChatRequest {
            model: self.model.clone(),
            messages: vec![
                OllamaMessage {
                    role: "system".to_string(),
                    content: request.system_prompt.clone(),
                },
                OllamaMessage {
                    role: "user".to_string(),
                    content: user,
                },
            ],
            stream: false,
        };
        let response = self
            .client
            .post(format!("{}/api/chat", self.base_url))
            .json(&body)
            .send()
            .map_err(|e| anyhow!("ollama request failed: {e}"))?;
        if !response.status().is_success() {
            return Err(anyhow!("ollama returned status {}", response.status()));
        }
        let parsed: OllamaChatResponse = response
            .json()
            .map_err(|e| anyhow!("decode ollama response: {e}"))?;
        Ok(parsed.message.content)
    }

    fn name(&self) -> &str {
        "ollama"
    }
}
