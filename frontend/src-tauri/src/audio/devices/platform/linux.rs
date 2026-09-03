use anyhow::Result;
use cpal::traits::{DeviceTrait, HostTrait};

use crate::audio::devices::configuration::{AudioDevice, DeviceType, LINUX_SYSTEM_AUDIO_DEVICE_NAME};

/// Configure Linux audio devices using ALSA/PulseAudio
pub fn configure_linux_audio(host: &cpal::Host) -> Result<Vec<AudioDevice>> {
    let mut devices = Vec::new();

    // Add input devices
    for device in host.input_devices()? {
        if let Ok(name) = device.name() {
            devices.push(AudioDevice::new(name, DeviceType::Input));
        }
    }

    // System audio is captured from the PulseAudio/PipeWire monitor of the default
    // sink. This entry is always available: it is handled by the Pulse backend and
    // never resolved through cpal.
    devices.push(AudioDevice::new(
        LINUX_SYSTEM_AUDIO_DEVICE_NAME.to_string(),
        DeviceType::Output,
    ));

    // Additionally expose any ALSA monitor sources cpal can see (less reliable)
    if let Ok(pulse_host) = cpal::host_from_id(cpal::HostId::Alsa) {
        for device in pulse_host.input_devices()? {
            if let Ok(name) = device.name() {
                // Check if it's a monitor source
                if name.contains("monitor") {
                    devices.push(AudioDevice::new(
                        format!("{} (System Audio)", name),
                        DeviceType::Output
                    ));
                }
            }
        }
    }

    Ok(devices)
}