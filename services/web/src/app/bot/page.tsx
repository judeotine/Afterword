'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';
import { ApiError, BotJob } from '@/lib/api';

function detectPlatform(url: string): string | null {
  const lower = url.toLowerCase();
  if (lower.includes('meet.google.com')) {
    return 'Google Meet';
  }
  if (lower.includes('zoom.us')) {
    return 'Zoom';
  }
  if (lower.includes('teams.microsoft.com') || lower.includes('teams.live.com')) {
    return 'Microsoft Teams';
  }
  return null;
}

export default function BotPage() {
  const { ready, signedIn, api, accessToken, workspaceId } = useSession();
  const router = useRouter();
  const [url, setUrl] = useState('');
  const [botName, setBotName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [jobs, setJobs] = useState<BotJob[]>([]);

  useEffect(() => {
    if (ready && !signedIn) {
      router.replace('/');
    }
  }, [ready, signedIn, router]);

  const loadJobs = useCallback(async () => {
    if (!workspaceId) {
      return;
    }
    try {
      const page = await api.listBotJobs({ accessToken: accessToken ?? undefined, workspaceId });
      setJobs(page.items);
    } catch {
      setJobs([]);
    }
  }, [api, accessToken, workspaceId]);

  useEffect(() => {
    if (signedIn) {
      void loadJobs();
    }
  }, [signedIn, loadJobs]);

  const platform = detectPlatform(url);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!workspaceId) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.sendBot(url.trim(), { accessToken: accessToken ?? undefined, workspaceId }, botName.trim() ? { bot_name: botName.trim() } : undefined);
      setUrl('');
      setBotName('');
      await loadJobs();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        setError('Only a workspace admin can send the notetaker to a meeting.');
      } else if (err instanceof ApiError && err.status === 402) {
        setError('This workspace does not have enough credits. Top up to send the notetaker.');
      } else {
        setError(err instanceof ApiError ? err.message : 'Could not send the notetaker');
      }
    } finally {
      setBusy(false);
    }
  };

  if (!ready) {
    return <main className="p-8 text-slate-500">Loading...</main>;
  }

  return (
    <main className="mx-auto max-w-2xl px-6 py-10">
      <h1 className="text-2xl font-semibold text-ink">Send the notetaker to a meeting</h1>
      <p className="mt-2 text-slate-500">
        Paste a Google Meet, Zoom or Teams link and the Afterword notetaker will join and record it.
      </p>

      <form onSubmit={submit} className="mt-8 space-y-4 rounded-lg border border-slate-200 bg-white p-6">
        <div className="space-y-2">
          <label className="block text-sm font-medium text-slate-700" htmlFor="meeting-url">
            Meeting link
          </label>
          <input
            id="meeting-url"
            value={url}
            onChange={(event) => setUrl(event.target.value)}
            placeholder="https://meet.google.com/abc-defg-hij"
            className="w-full rounded-md border border-slate-300 px-3 py-2"
            required
          />
          {url.trim() ? (
            platform ? (
              <p className="text-xs text-emerald-600">Detected {platform}</p>
            ) : (
              <p className="text-xs text-amber-600">That does not look like a Meet, Zoom or Teams link.</p>
            )
          ) : null}
        </div>

        <div className="space-y-2">
          <label className="block text-sm font-medium text-slate-700" htmlFor="bot-name">
            Notetaker name (optional)
          </label>
          <input
            id="bot-name"
            value={botName}
            onChange={(event) => setBotName(event.target.value)}
            placeholder="Afterword Notetaker"
            className="w-full rounded-md border border-slate-300 px-3 py-2"
          />
        </div>

        {error ? <p className="text-sm text-red-600">{error}</p> : null}

        <button
          type="submit"
          disabled={busy || !url.trim() || !platform}
          className="rounded-md bg-accent px-4 py-2 font-medium text-white disabled:opacity-60"
        >
          {busy ? 'Sending...' : 'Send notetaker'}
        </button>
      </form>

      <section className="mt-10">
        <h2 className="text-lg font-medium text-ink">Recent notetaker sessions</h2>
        {jobs.length === 0 ? (
          <p className="mt-3 text-slate-500">No notetaker sessions yet.</p>
        ) : (
          <ul className="mt-4 space-y-3">
            {jobs.map((job) => (
              <li key={job.id} className="rounded-lg border border-slate-200 bg-white px-4 py-3">
                <div className="flex items-center justify-between">
                  <span className="truncate font-medium text-ink">{job.meeting_url}</span>
                  <span className="ml-3 shrink-0 text-xs uppercase tracking-wide text-slate-400">{job.status}</span>
                </div>
                <span className="text-xs text-slate-400">{job.platform}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
