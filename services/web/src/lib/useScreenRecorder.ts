'use client';

import { useCallback, useRef, useState } from 'react';

export type RecorderState = 'idle' | 'recording' | 'stopped';

export interface ScreenRecorderResult {
  state: RecorderState;
  error: string | null;
  blob: Blob | null;
  durationMs: number;
  start: (options?: { withMicrophone?: boolean }) => Promise<void>;
  stop: () => void;
  reset: () => void;
}

function pickMimeType(): string {
  const candidates = [
    'video/mp4;codecs=h264,aac',
    'video/webm;codecs=vp9,opus',
    'video/webm;codecs=vp8,opus',
    'video/webm',
  ];
  for (const candidate of candidates) {
    if (typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(candidate)) {
      return candidate;
    }
  }
  return 'video/webm';
}

export function useScreenRecorder(): ScreenRecorderResult {
  const [state, setState] = useState<RecorderState>('idle');
  const [error, setError] = useState<string | null>(null);
  const [blob, setBlob] = useState<Blob | null>(null);
  const [durationMs, setDurationMs] = useState(0);

  const recorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const streamsRef = useRef<MediaStream[]>([]);
  const startedAtRef = useRef<number>(0);

  const cleanup = useCallback(() => {
    streamsRef.current.forEach((stream) => stream.getTracks().forEach((track) => track.stop()));
    streamsRef.current = [];
    recorderRef.current = null;
  }, []);

  const start = useCallback(
    async (options?: { withMicrophone?: boolean }) => {
      setError(null);
      setBlob(null);
      chunksRef.current = [];
      try {
        const display = await navigator.mediaDevices.getDisplayMedia({
          video: { frameRate: 30 },
          audio: true,
        });
        streamsRef.current = [display];

        const tracks = [...display.getTracks()];

        if (options?.withMicrophone) {
          try {
            const mic = await navigator.mediaDevices.getUserMedia({ audio: true });
            streamsRef.current.push(mic);
            mic.getAudioTracks().forEach((track) => tracks.push(track));
          } catch (micError) {
            console.warn('Microphone capture was declined:', micError);
          }
        }

        const combined = new MediaStream(tracks);
        const mimeType = pickMimeType();
        const recorder = new MediaRecorder(combined, { mimeType });
        recorderRef.current = recorder;

        recorder.ondataavailable = (event) => {
          if (event.data.size > 0) {
            chunksRef.current.push(event.data);
          }
        };
        recorder.onstop = () => {
          const finished = new Blob(chunksRef.current, { type: mimeType });
          setBlob(finished);
          setDurationMs(Date.now() - startedAtRef.current);
          setState('stopped');
          cleanup();
        };

        display.getVideoTracks().forEach((track) => {
          track.onended = () => {
            if (recorderRef.current && recorderRef.current.state !== 'inactive') {
              recorderRef.current.stop();
            }
          };
        });

        startedAtRef.current = Date.now();
        recorder.start(1000);
        setState('recording');
      } catch (err) {
        cleanup();
        setError(err instanceof Error ? err.message : 'Could not start screen recording');
        setState('idle');
      }
    },
    [cleanup],
  );

  const stop = useCallback(() => {
    if (recorderRef.current && recorderRef.current.state !== 'inactive') {
      recorderRef.current.stop();
    }
  }, []);

  const reset = useCallback(() => {
    cleanup();
    chunksRef.current = [];
    setBlob(null);
    setError(null);
    setDurationMs(0);
    setState('idle');
  }, [cleanup]);

  return { state, error, blob, durationMs, start, stop, reset };
}
