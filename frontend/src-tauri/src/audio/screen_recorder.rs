use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;

use anyhow::{anyhow, Result};

use super::ffmpeg::find_ffmpeg_path;

static SCREEN_RECORDER: Mutex<Option<ScreenRecording>> = Mutex::new(None);

#[cfg(target_os = "macos")]
mod macos_screen {
    #[link(name = "CoreGraphics", kind = "framework")]
    extern "C" {
        fn CGPreflightScreenCaptureAccess() -> bool;
        fn CGRequestScreenCaptureAccess() -> bool;
    }

    pub fn has_access() -> bool {
        unsafe { CGPreflightScreenCaptureAccess() }
    }

    pub fn request_access() -> bool {
        unsafe { CGRequestScreenCaptureAccess() }
    }
}

pub fn ensure_screen_permission() -> bool {
    #[cfg(target_os = "macos")]
    {
        if macos_screen::has_access() {
            return true;
        }
        return macos_screen::request_access();
    }
    #[cfg(not(target_os = "macos"))]
    {
        true
    }
}

struct ScreenRecording {
    child: Child,
    output_path: PathBuf,
}

pub fn is_screen_recording() -> bool {
    SCREEN_RECORDER
        .lock()
        .map(|guard| guard.is_some())
        .unwrap_or(false)
}

pub fn detect_screen_index() -> Result<u32> {
    let ffmpeg_path = find_ffmpeg_path().ok_or_else(|| anyhow!("ffmpeg binary was not found"))?;
    let output = Command::new(ffmpeg_path)
        .arg("-f")
        .arg("avfoundation")
        .arg("-list_devices")
        .arg("true")
        .arg("-i")
        .arg("")
        .output()
        .map_err(|e| anyhow!("failed to list avfoundation devices: {}", e))?;

    let text = String::from_utf8_lossy(&output.stderr);
    let mut in_video_section = false;
    for line in text.lines() {
        if line.contains("AVFoundation video devices:") {
            in_video_section = true;
            continue;
        }
        if line.contains("AVFoundation audio devices:") {
            in_video_section = false;
            continue;
        }
        if in_video_section && line.to_lowercase().contains("capture screen") {
            if let Some(index) = parse_device_index(line) {
                return Ok(index);
            }
        }
    }
    Err(anyhow!("no screen capture device was found"))
}

pub fn detect_system_audio_index() -> Option<u32> {
    let ffmpeg_path = find_ffmpeg_path()?;
    let output = Command::new(ffmpeg_path)
        .arg("-f")
        .arg("avfoundation")
        .arg("-list_devices")
        .arg("true")
        .arg("-i")
        .arg("")
        .output()
        .ok()?;

    let text = String::from_utf8_lossy(&output.stderr);
    let mut in_audio_section = false;
    let mut first_index: Option<u32> = None;
    for line in text.lines() {
        if line.contains("AVFoundation audio devices:") {
            in_audio_section = true;
            continue;
        }
        if in_audio_section && line.starts_with('[') {
            let lower = line.to_lowercase();
            let index = parse_device_index(line);
            if index.is_none() {
                continue;
            }
            if first_index.is_none() {
                first_index = index;
            }
            if lower.contains("blackhole")
                || lower.contains("aggregate")
                || lower.contains("loopback")
                || lower.contains("eshareaudio")
            {
                return index;
            }
        }
    }
    None
}

fn parse_device_index(line: &str) -> Option<u32> {
    let start = line.rfind('[')?;
    let end = line[start..].find(']')? + start;
    line[start + 1..end].trim().parse::<u32>().ok()
}

pub fn start_screen_recording(
    output_path: PathBuf,
    screen_index: Option<u32>,
    audio_index: Option<u32>,
) -> Result<()> {
    let mut guard = SCREEN_RECORDER
        .lock()
        .map_err(|_| anyhow!("screen recorder lock poisoned"))?;
    if guard.is_some() {
        return Err(anyhow!("a screen recording is already in progress"));
    }

    if !ensure_screen_permission() {
        return Err(anyhow!(
            "screen recording permission was not granted. Enable it in System Settings, Privacy and Security, Screen Recording"
        ));
    }

    let ffmpeg_path = find_ffmpeg_path().ok_or_else(|| anyhow!("ffmpeg binary was not found"))?;

    let video = match screen_index {
        Some(index) => index,
        None => detect_screen_index()?,
    };
    let audio = match audio_index {
        Some(index) => Some(index),
        None => detect_system_audio_index(),
    };

    let input = match audio {
        Some(a) => format!("{}:{}", video, a),
        None => format!("{}:none", video),
    };

    let mut command = Command::new(ffmpeg_path);
    command
        .arg("-y")
        .arg("-f")
        .arg("avfoundation")
        .arg("-framerate")
        .arg("30")
        .arg("-capture_cursor")
        .arg("1")
        .arg("-i")
        .arg(&input)
        .arg("-vf")
        .arg("format=yuv420p")
        .arg("-c:v")
        .arg("h264")
        .arg("-preset")
        .arg("veryfast")
        .arg("-movflags")
        .arg("+faststart");

    if audio.is_some() {
        command.arg("-c:a").arg("aac").arg("-b:a").arg("128k");
    }

    command
        .arg(&output_path)
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .stderr(Stdio::null());

    let child = command
        .spawn()
        .map_err(|e| anyhow!("failed to start ffmpeg screen capture: {}", e))?;

    *guard = Some(ScreenRecording {
        child,
        output_path,
    });
    Ok(())
}

pub fn stop_screen_recording() -> Result<Option<PathBuf>> {
    let mut guard = SCREEN_RECORDER
        .lock()
        .map_err(|_| anyhow!("screen recorder lock poisoned"))?;
    let mut recording = match guard.take() {
        Some(recording) => recording,
        None => return Ok(None),
    };

    use std::io::Write;
    if let Some(stdin) = recording.child.stdin.as_mut() {
        let _ = stdin.write_all(b"q");
        let _ = stdin.flush();
    }

    match recording.child.wait() {
        Ok(_) => {}
        Err(_) => {
            let _ = recording.child.kill();
            let _ = recording.child.wait();
        }
    }

    Ok(Some(recording.output_path))
}
