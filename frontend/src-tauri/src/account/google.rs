use std::time::Duration;

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;

use crate::database::secrets::default_store;

const ACCOUNT_REFRESH_KEY: &str = "account_refresh_token";

#[derive(serde::Serialize)]
pub struct GoogleSignInResult {
    pub access_token: String,
    pub refresh_token: String,
}

fn parse_tokens(request_line: &str) -> Option<(String, String)> {
    let path = request_line.split_whitespace().nth(1)?;
    let query = path.split('?').nth(1)?;
    let mut access = None;
    let mut refresh = None;
    for pair in query.split('&') {
        let mut parts = pair.splitn(2, '=');
        let key = parts.next().unwrap_or("");
        let value = parts.next().unwrap_or("");
        let decoded = urldecode(value);
        match key {
            "access_token" => access = Some(decoded),
            "refresh_token" => refresh = Some(decoded),
            _ => {}
        }
    }
    match (access, refresh) {
        (Some(a), Some(r)) if !a.is_empty() && !r.is_empty() => Some((a, r)),
        _ => None,
    }
}

fn urldecode(input: &str) -> String {
    let bytes = input.replace('+', " ");
    let bytes = bytes.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' && i + 2 < bytes.len() {
            let hi = (bytes[i + 1] as char).to_digit(16);
            let lo = (bytes[i + 2] as char).to_digit(16);
            if let (Some(hi), Some(lo)) = (hi, lo) {
                out.push((hi * 16 + lo) as u8);
                i += 3;
                continue;
            }
        }
        out.push(bytes[i]);
        i += 1;
    }
    String::from_utf8_lossy(&out).to_string()
}

const SUCCESS_HTML: &str = "<!doctype html><html><head><meta charset=\"utf-8\"><title>Afterword</title><style>body{font-family:-apple-system,system-ui,sans-serif;display:flex;height:100vh;margin:0;align-items:center;justify-content:center;background:#f8fafc;color:#0f172a}div{text-align:center}</style></head><body><div><h2>You are signed in</h2><p>Return to the Afterword app. You can close this tab.</p></div></body></html>";

#[tauri::command]
pub async fn account_google_sign_in(api_base_url: String) -> Result<GoogleSignInResult, String> {
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .map_err(|e| format!("could not open a local callback port: {}", e))?;
    let port = listener
        .local_addr()
        .map_err(|e| e.to_string())?
        .port();

    let base = api_base_url.trim_end_matches('/');
    let start_url = format!("{}/v1/auth/google/start?desktop_port={}", base, port);
    open_in_browser(&start_url)?;

    let accept = tokio::time::timeout(Duration::from_secs(300), listener.accept()).await;
    let (mut stream, _) = accept
        .map_err(|_| "sign-in timed out. Please try again.".to_string())?
        .map_err(|e| format!("callback connection failed: {}", e))?;

    let mut buffer = [0u8; 4096];
    let read = stream
        .read(&mut buffer)
        .await
        .map_err(|e| format!("could not read the callback: {}", e))?;
    let request = String::from_utf8_lossy(&buffer[..read]);
    let request_line = request.lines().next().unwrap_or("");

    let tokens = parse_tokens(request_line);

    let body = SUCCESS_HTML.as_bytes();
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    let _ = stream.write_all(response.as_bytes()).await;
    let _ = stream.write_all(body).await;
    let _ = stream.flush().await;

    let (access_token, refresh_token) =
        tokens.ok_or_else(|| "sign-in did not return the expected tokens".to_string())?;

    default_store()
        .set(ACCOUNT_REFRESH_KEY, &refresh_token)
        .map_err(|e| e.to_string())?;

    Ok(GoogleSignInResult {
        access_token,
        refresh_token,
    })
}

fn open_in_browser(url: &str) -> Result<(), String> {
    use std::process::Command;
    let result = if cfg!(target_os = "windows") {
        Command::new("cmd").args(["/C", "start", "", url]).spawn()
    } else if cfg!(target_os = "macos") {
        Command::new("open").arg(url).spawn()
    } else {
        Command::new("xdg-open").arg(url).spawn()
    };
    result
        .map(|_| ())
        .map_err(|e| format!("could not open the browser: {}", e))
}
