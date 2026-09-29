mod provider;
mod template;

#[cfg(test)]
mod tests;

pub use provider::{OfflineProvider, OllamaProvider, SummaryProvider, SummaryRequest};
pub use template::{build_final_report_system_prompt, Template, TemplateSection};

use anyhow::Result;

pub fn summarize_transcript(
    template: &Template,
    transcript: &str,
    provider: &dyn SummaryProvider,
) -> Result<String> {
    template
        .validate()
        .map_err(|e| anyhow::anyhow!("invalid template: {e}"))?;
    let system_prompt = build_final_report_system_prompt(
        &template.to_section_instructions(),
        &template.to_markdown_structure(),
    );
    let request = SummaryRequest {
        system_prompt,
        transcript: transcript.to_string(),
        template_markdown: template.to_markdown_structure(),
    };
    provider.summarize(&request)
}
