use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;

use anyhow::{anyhow, Result};

use super::ffmpeg::find_ffmpeg_path;

static SCREEN_RECORDER: Mutex<Option<ScreenRecording>> = Mutex::new(None);

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

pub fn start_screen_recording(output_path: PathBuf, screen_index: u32, audio_index: Option<u32>) -> Result<()> {
    let mut guard = SCREEN_RECORDER
        .lock()
        .map_err(|_| anyhow!("screen recorder lock poisoned"))?;
    if guard.is_some() {
        return Err(anyhow!("a screen recording is already in progress"));
    }

    let ffmpeg_path = find_ffmpeg_path().ok_or_else(|| anyhow!("ffmpeg binary was not found"))?;

    let input = match audio_index {
        Some(audio) => format!("{}:{}", screen_index, audio),
        None => format!("{}:none", screen_index),
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
        .arg("-c:v")
        .arg("h264")
        .arg("-pix_fmt")
        .arg("yuv420p")
        .arg("-preset")
        .arg("veryfast")
        .arg("-movflags")
        .arg("+faststart");

    if audio_index.is_some() {
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
