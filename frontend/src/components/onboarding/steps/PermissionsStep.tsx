import React, { useEffect, useState, useCallback } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { Bell, Mic, Volume2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { OnboardingContainer } from '../OnboardingContainer';
import { PermissionRow } from '../shared';
import { useOnboarding } from '@/contexts/OnboardingContext';

export function PermissionsStep() {
  const { setPermissionStatus, setPermissionsSkipped, permissions, completeOnboarding } = useOnboarding();
  const [isPending, setIsPending] = useState(false);
  // Desktop notifications are opt-in: off until the user turns them on here.
  const [notificationsConsent, setNotificationsConsent] = useState(false);

  // Check permissions - only logs current state, doesn't auto-authorize
  // Actual permission checks are done via explicit user actions (clicking Enable)
  const checkPermissions = useCallback(async () => {
    console.log('[PermissionsStep] Current permission states:');
    console.log(`  - Microphone: ${permissions.microphone}`);
    console.log(`  - System Audio: ${permissions.systemAudio}`);
    // Don't auto-set permissions based on device availability
    // Permissions should only be set after explicit user action via Enable button
  }, [permissions.microphone, permissions.systemAudio]);

  // Check permissions on mount
  useEffect(() => {
    checkPermissions();
  }, [checkPermissions]);

  // Request microphone permission
  const handleMicrophoneAction = async () => {
    if (permissions.microphone === 'denied') {
      // Try to open system settings
      try {
        await invoke('open_system_settings');
      } catch {
        alert('Please enable microphone access in System Preferences > Security & Privacy > Microphone');
      }
      return;
    }

    setIsPending(true);
    try {
      console.log('[PermissionsStep] Triggering microphone permission...');
      const granted = await invoke<boolean>('trigger_microphone_permission');
      console.log('[PermissionsStep] Microphone permission result:', granted);

      if (granted) {
        setPermissionStatus('microphone', 'authorized');
      } else {
        // Permission was denied or dialog was dismissed
        setPermissionStatus('microphone', 'denied');
      }
    } catch (err) {
      console.error('[PermissionsStep] Failed to request microphone permission:', err);
      setPermissionStatus('microphone', 'denied');
    } finally {
      setIsPending(false);
    }
  };

  // Request system audio permission
  const handleSystemAudioAction = async () => {
    if (permissions.systemAudio === 'denied') {
      // Try to open system settings
      try {
        await invoke('open_system_settings');
      } catch {
        alert('Please enable Audio Capture in System Settings → Privacy & Security → Audio Capture');
      }
      return;
    }

    setIsPending(true);
    try {
      console.log('[PermissionsStep] Triggering Audio Capture permission...');
      // Backend creates Core Audio tap, captures audio, and verifies it's not silence
      // Returns true if permission granted and audio verified, false if denied (silence)
      const granted = await invoke<boolean>('trigger_system_audio_permission_command');
      console.log('[PermissionsStep] System audio permission result:', granted);

      if (granted) {
        setPermissionStatus('systemAudio', 'authorized');
        console.log('[PermissionsStep] Audio Capture permission verified - audio is not silence');
      } else {
        // Permission was denied (audio is silence)
        setPermissionStatus('systemAudio', 'denied');
        console.log('[PermissionsStep] Audio Capture permission denied - audio is silence');
      }
    } catch (err) {
      console.error('[PermissionsStep] Failed to request system audio permission:', err);
      setPermissionStatus('systemAudio', 'denied');
    } finally {
      setIsPending(false);
    }
  };

  // Store notification consent immediately so it survives skipping the step
  const handleNotificationsConsentChange = async (consent: boolean) => {
    setNotificationsConsent(consent);
    try {
      await invoke('set_notification_consent', { consent });
    } catch (err) {
      console.error('[PermissionsStep] Failed to set notification consent:', err);
      setNotificationsConsent(!consent);
    }
  };

  const handleFinish = async () => {
    try {
      await completeOnboarding();
      window.location.reload();
    } catch (error) {
      console.error('Failed to complete onboarding:', error);
    }
  };

  const handleSkip = async () => {
    setPermissionsSkipped(true);
    await handleFinish();
  };

  const allPermissionsGranted =
    permissions.microphone === 'authorized' &&
    permissions.systemAudio === 'authorized';

  return (
    <OnboardingContainer
      title="Grant Permissions"
      description="Afterword needs access to your microphone and system audio to record meetings"
      step={4}
      hideProgress={true}
      showNavigation={allPermissionsGranted}
      canGoNext={allPermissionsGranted}
    >
      <div className="max-w-lg mx-auto space-y-6">
        {/* Permission Rows */}
        <div className="space-y-4">
          {/* Microphone */}
          <PermissionRow
            icon={<Mic className="w-5 h-5" />}
            title="Microphone"
            description="Required to capture your voice during meetings"
            status={permissions.microphone}
            isPending={isPending}
            onAction={handleMicrophoneAction}
          />

          {/* System Audio */}
          <PermissionRow
            icon={<Volume2 className="w-5 h-5" />}
            title="System Audio"
            description="Click Enable to grant Audio Capture permission"
            status={permissions.systemAudio}
            isPending={isPending}
            onAction={handleSystemAudioAction}
          />

          {/* Desktop notifications (optional, off by default) */}
          <div className="flex items-center justify-between gap-4 rounded-lg border border-neutral-200 p-4">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 text-neutral-600">
                <Bell className="w-5 h-5" />
              </div>
              <div>
                <div className="text-sm font-medium">Desktop notifications</div>
                <div className="text-xs text-muted-foreground">
                  Optional. Get notified when recording starts and stops.
                </div>
              </div>
            </div>
            <Switch
              checked={notificationsConsent}
              onCheckedChange={handleNotificationsConsentChange}
              aria-label="Desktop notifications"
            />
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex flex-col gap-3 pt-4">
          <Button onClick={handleFinish} disabled={!allPermissionsGranted} className="w-full h-11">
            Finish Setup
          </Button>

          <button
            onClick={handleSkip}
            className="text-sm text-neutral-500 hover:text-neutral-700 transition-colors"
          >
            I&apos;ll do this later
          </button>

          {!allPermissionsGranted && (
            <p className="text-xs text-center text-muted-foreground">
              Recording won&apos;t work without permissions. You can grant them later in settings.
            </p>
          )}
        </div>
      </div>
    </OnboardingContainer>
  );
}
