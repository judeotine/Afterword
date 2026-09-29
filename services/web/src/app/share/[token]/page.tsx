'use client';

import { useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import { ApiClient, Meeting, Summary, TranscriptSegment } from '@/lib/api';

function timestamp(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export default function SharePage() {
  const params = useParams<{ token: string }>();
  const token = params.token;
  const [meeting, setMeeting] = useState<Meeting | null>(null);
  const [segments, setSegments] = useState<TranscriptSegment[]>([]);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const api = new ApiClient();
    api
      .getSharedMeeting(token)
      .then((result) => {
        if (cancelled) {
          return;
        }
        setMeeting(result.meeting);
        setSegments(result.segments);
        setSummary(result.summary);
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'This share link is not available');
        }
      });
    return () => {
      cancelled = true;
    };
  }, [token]);

  if (error) {
    return <main className="mx-auto max-w-2xl px-6 py-16 text-slate-600">{error}</main>;
  }

  return (
    <main className="mx-auto max-w-2xl px-6 py-12">
      <h1 className="text-2xl font-semibold text-ink">{meeting?.title || 'Shared meeting'}</h1>
      {summary ? (
        <section className="mt-6 whitespace-pre-wrap text-slate-700">{summary.content}</section>
      ) : null}
      <section className="mt-8 space-y-3">
        {segments.map((segment) => (
          <p key={segment.id} className="text-slate-700">
            <span className="mr-2 text-xs text-slate-400">{timestamp(segment.start_ms)}</span>
            {segment.speaker ? <span className="mr-1 font-medium text-ink">{segment.speaker}:</span> : null}
            {segment.text}
          </p>
        ))}
      </section>
    </main>
  );
}
