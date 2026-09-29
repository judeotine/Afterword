use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TemplateSection {
    pub title: String,
    pub instruction: String,
    pub format: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub item_format: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub example_item_format: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Template {
    pub name: String,
    pub description: String,
    pub sections: Vec<TemplateSection>,
}

impl Template {
    pub fn validate(&self) -> Result<(), String> {
        if self.name.is_empty() {
            return Err("Template name cannot be empty".to_string());
        }
        if self.description.is_empty() {
            return Err("Template description cannot be empty".to_string());
        }
        if self.sections.is_empty() {
            return Err("Template must have at least one section".to_string());
        }
        for (i, section) in self.sections.iter().enumerate() {
            if section.title.is_empty() {
                return Err(format!("Section {} has empty title", i));
            }
            if section.instruction.is_empty() {
                return Err(format!("Section '{}' has empty instruction", section.title));
            }
            match section.format.as_str() {
                "paragraph" | "list" | "string" => {}
                other => {
                    return Err(format!(
                        "Section '{}' has invalid format '{}'. Must be 'paragraph', 'list', or 'string'",
                        section.title, other
                    ))
                }
            }
        }
        Ok(())
    }

    pub fn to_markdown_structure(&self) -> String {
        let mut markdown = String::from("# <Add Title here>\n\n");
        for section in &self.sections {
            markdown.push_str(&format!("**{}**\n\n", section.title));
        }
        markdown
    }

    pub fn to_section_instructions(&self) -> String {
        let mut instructions = String::from(
            "- **For the main title (`# [AI-Generated Title]`):** Analyze the entire transcript and create a concise, descriptive title for the meeting.\n",
        );
        for section in &self.sections {
            instructions.push_str(&format!(
                "- **For the '{}' section:** {}.\n",
                section.title, section.instruction
            ));
            let item_format = section
                .item_format
                .as_ref()
                .or(section.example_item_format.as_ref());
            if let Some(format) = item_format {
                instructions.push_str(&format!(
                    "  - Items in this section should follow the format: `{}`.\n",
                    format
                ));
            }
        }
        instructions
    }
}

pub fn build_final_report_system_prompt(
    section_instructions: &str,
    template_markdown: &str,
) -> String {
    format!(
        r#"You are an expert meeting summarizer. Generate a final meeting report by filling in the provided Markdown template based on the source text.

Rules:
1. Use only information supported by the transcript.
2. Keep the exact section headings from the template.
3. Do not invent facts, owners, or dates.
4. Fill each template section per its instructions.

Section instructions:
{section_instructions}

<template>
{template_markdown}</template>"#
    )
}
