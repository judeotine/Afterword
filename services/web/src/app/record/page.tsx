'use client';

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';
import { useScreenRecorder } from '@/lib/useScreenRecorder';

function formatDuration(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export default function RecordPage() {
  const { ready, signedIn } = useSession();
  const router = useRouter();
  const { state, error, blob, durationMs, start, stop, reset } = useScreenRecorder();
  const [withMic, setWithMic] = useState(true);
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (ready && !signedIn) {
      router.replace('/');
    }
  }, [ready, signedIn, router]);

  useEffect(() => {
    if (state !== 'recording') {
      return;
    }
    const started = Date.now();
    const timer = setInterval(() => setElapsed(Date.now() - started), 500);
    return () => clearInterval(timer);
  }, [state]);

  const downloadUrl = useMemo(() => (blob ? URL.createObjectURL(blob) : null), [blob]);
  useEffect(() => {
    return () => {
      if (downloadUrl) {
        URL.revokeObjectURL(downloadUrl);
      }
    };
  }, [downloadUrl]);

  const extension = blob?.type.includes('mp4') ? 'mp4' : 'webm';

  if (!ready) {
    return <main className="p-8 text-slate-500">Loading...</main>;
  }

  return (
    <main className="mx-auto max-w-2xl px-6 py-10">
      <h1 className="text-2xl font-semibold text-ink">Record a meeting</h1>
      <p className="mt-2 text-slate-500">
        Capture your screen with system and microphone audio, straight from the browser.
      </p>

      <div className="mt-8 rounded-lg border border-slate-200 bg-white p-6">
        {state === 'recording' ? (
          <div className="flex items-center gap-3">
            <span className="h-3 w-3 animate-pulse rounded-full bg-red-500" />
            <span className="font-medium text-ink">Recording {formatDuration(elapsed)}</span>
          </div>
        ) : (
          <label className="flex items-center gap-2 text-sm text-slate-700">
            <input
              type="checkbox"
              checked={withMic}
              onChange={(event) => setWithMic(event.target.checked)}
            />
            Include microphone audio
          </label>
        )}

        <div className="mt-6 flex gap-3">
          {state !== 'recording' ? (
            <button
              onClick={() => start({ withMicrophone: withMic })}
              className="rounded-md bg-accent px-4 py-2 font-medium text-white"
            >
              {state === 'stopped' ? 'Record again' : 'Start recording'}
            </button>
          ) : (
            <button
              onClick={stop}
              className="rounded-md bg-red-600 px-4 py-2 font-medium text-white"
            >
              Stop recording
            </button>
          )}
          {state === 'stopped' ? (
            <button onClick={reset} className="rounded-md border border-slate-300 px-4 py-2 text-sm">
              Discard
            </button>
          ) : null}
        </div>

        {error ? <p className="mt-4 text-sm text-red-600">{error}</p> : null}
      </div>

      {blob && downloadUrl ? (
        <div className="mt-8 rounded-lg border border-slate-200 bg-white p-6">
          <h2 className="text-lg font-medium text-ink">Recording ready</h2>
          <p className="mt-1 text-sm text-slate-500">Length {formatDuration(durationMs)}</p>
          <video src={downloadUrl} controls className="mt-4 w-full rounded-md" />
          <a
            href={downloadUrl}
            download={`afterword-recording.${extension}`}
            className="mt-4 inline-block rounded-md bg-accent px-4 py-2 font-medium text-white"
          >
            Download recording
          </a>
        </div>
      ) : null}
    </main>
  );
}
