use super::*;

fn sample_template() -> Template {
    Template {
        name: "Test".to_string(),
        description: "A test template".to_string(),
        sections: vec![
            TemplateSection {
                title: "Summary".to_string(),
                instruction: "Summarize the meeting".to_string(),
                format: "paragraph".to_string(),
                item_format: None,
                example_item_format: None,
            },
            TemplateSection {
                title: "Action Items".to_string(),
                instruction: "List the tasks".to_string(),
                format: "list".to_string(),
                item_format: Some("| Owner | Task |".to_string()),
                example_item_format: None,
            },
        ],
    }
}

#[test]
fn markdown_structure_lists_each_section() {
    let template = sample_template();
    let markdown = template.to_markdown_structure();
    assert!(markdown.contains("**Summary**"));
    assert!(markdown.contains("**Action Items**"));
}

#[test]
fn section_instructions_include_item_format() {
    let template = sample_template();
    let instructions = template.to_section_instructions();
    assert!(instructions.contains("'Summary' section"));
    assert!(instructions.contains("| Owner | Task |"));
}

#[test]
fn offline_provider_fills_every_section() {
    let template = sample_template();
    let output = summarize_transcript(&template, "some transcript text", &OfflineProvider).unwrap();
    assert!(output.contains("## Summary"));
    assert!(output.contains("## Action Items"));
    assert!(output.starts_with("# Meeting Summary"));
}

#[test]
fn summarize_rejects_invalid_template() {
    let template = Template {
        name: String::new(),
        description: "x".to_string(),
        sections: vec![],
    };
    assert!(summarize_transcript(&template, "text", &OfflineProvider).is_err());
}
