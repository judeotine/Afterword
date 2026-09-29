'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';
import { Meeting } from '@/lib/api';

function formatDuration(seconds: number | null): string {
  if (!seconds || seconds <= 0) {
    return '--';
  }
  const minutes = Math.round(seconds / 60);
  return `${minutes} min`;
}

export default function MeetingsPage() {
  const { ready, signedIn, api, accessToken, workspaceId, workspaces, selectWorkspace, signOut } = useSession();
  const router = useRouter();
  const [meetings, setMeetings] = useState<Meeting[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (ready && !signedIn) {
      router.replace('/');
    }
  }, [ready, signedIn, router]);

  useEffect(() => {
    if (!signedIn || !workspaceId) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .listMeetings(null, { accessToken: accessToken ?? undefined, workspaceId })
      .then((page) => {
        if (cancelled) {
          return;
        }
        setMeetings(page.items);
        setCursor(page.next_cursor);
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Could not load meetings');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [api, signedIn, accessToken, workspaceId]);

  const loadMore = async () => {
    if (!cursor || !workspaceId) {
      return;
    }
    setLoading(true);
    try {
      const page = await api.listMeetings(cursor, { accessToken: accessToken ?? undefined, workspaceId });
      setMeetings((current) => [...current, ...page.items]);
      setCursor(page.next_cursor);
    } finally {
      setLoading(false);
    }
  };

  if (!ready) {
    return <main className="p-8 text-slate-500">Loading...</main>;
  }

  return (
    <main className="mx-auto max-w-4xl px-6 py-10">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-ink">Meetings</h1>
        <div className="flex items-center gap-3">
          <Link
            href="/record"
            className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white"
          >
            Record
          </Link>
          <Link
            href="/bot"
            className="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-ink"
          >
            Send notetaker
          </Link>
          {workspaces.length > 1 ? (
            <select
              className="rounded-md border border-slate-300 px-2 py-1 text-sm"
              value={workspaceId ?? ''}
              onChange={(event) => selectWorkspace(event.target.value)}
            >
              {workspaces.map((workspace) => (
                <option key={workspace.id} value={workspace.id}>
                  {workspace.name}
                </option>
              ))}
            </select>
          ) : null}
          <button className="text-sm text-slate-500" onClick={signOut}>
            Sign out
          </button>
        </div>
      </header>

      {error ? <p className="mt-6 text-sm text-red-600">{error}</p> : null}

      <ul className="mt-8 space-y-3">
        {meetings.map((meeting) => (
          <li key={meeting.id}>
            <Link
              href={`/meetings/${meeting.id}`}
              className="block rounded-lg border border-slate-200 bg-white px-4 py-3 hover:border-accent"
            >
              <div className="flex items-center justify-between">
                <span className="font-medium text-ink">{meeting.title || 'Untitled meeting'}</span>
                <span className="text-sm text-slate-400">{formatDuration(meeting.duration_seconds)}</span>
              </div>
              <span className="text-xs uppercase tracking-wide text-slate-400">{meeting.status}</span>
            </Link>
          </li>
        ))}
      </ul>

      {meetings.length === 0 && !loading ? (
        <p className="mt-8 text-slate-500">No meetings yet.</p>
      ) : null}

      {cursor ? (
        <button
          onClick={loadMore}
          disabled={loading}
          className="mt-6 rounded-md border border-slate-300 px-4 py-2 text-sm disabled:opacity-60"
        >
          {loading ? 'Loading...' : 'Load more'}
        </button>
      ) : null}
    </main>
  );
}
