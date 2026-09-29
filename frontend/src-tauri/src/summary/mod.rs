use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CustomOpenAIConfig {
    pub endpoint: String,

    #[serde(rename = "apiKey")]
    pub api_key: Option<String>,

    pub model: String,

    #[serde(rename = "maxTokens")]
    pub max_tokens: Option<i32>,

    pub temperature: Option<f32>,

    #[serde(rename = "topP")]
    pub top_p: Option<f32>,
}

pub mod commands;
pub(crate) mod language_detection;
pub mod llm_client;
pub(crate) mod metadata;
pub mod processor;
pub mod service;
pub mod summary_engine;
pub mod template_commands;
pub mod templates;

pub use commands::{
    __cmd__api_cancel_summary, __cmd__api_detect_transcript_summary_language,
    __cmd__api_get_meeting_detected_summary_language, __cmd__api_get_meeting_summary_language,
    __cmd__api_get_summary, __cmd__api_process_transcript,
    __cmd__api_save_meeting_detected_summary_language, __cmd__api_save_meeting_summary,
    __cmd__api_save_meeting_summary_language, __tauri_command_name_api_cancel_summary,
    __tauri_command_name_api_detect_transcript_summary_language,
    __tauri_command_name_api_get_meeting_detected_summary_language,
    __tauri_command_name_api_get_meeting_summary_language, __tauri_command_name_api_get_summary,
    __tauri_command_name_api_process_transcript,
    __tauri_command_name_api_save_meeting_detected_summary_language,
    __tauri_command_name_api_save_meeting_summary,
    __tauri_command_name_api_save_meeting_summary_language, api_cancel_summary,
    api_detect_transcript_summary_language, api_get_meeting_detected_summary_language,
    api_get_meeting_summary_language, api_get_summary, api_process_transcript,
    api_save_meeting_detected_summary_language, api_save_meeting_summary,
    api_save_meeting_summary_language,
};

pub use template_commands::{
    __cmd__api_get_template_details, __cmd__api_list_templates, __cmd__api_validate_template,
    __tauri_command_name_api_get_template_details, __tauri_command_name_api_list_templates,
    __tauri_command_name_api_validate_template, api_get_template_details, api_list_templates,
    api_validate_template,
};

pub use llm_client::LLMProvider;
pub use processor::{
    chunk_text, clean_llm_markdown_output, extract_meeting_name_from_markdown,
    generate_meeting_summary, rough_token_count,
};
pub use service::SummaryService;
