'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';
import { AskAnswer, Meeting, Summary, TranscriptSegment } from '@/lib/api';

function timestamp(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export default function MeetingDetailPage() {
  const params = useParams<{ id: string }>();
  const meetingId = params.id;
  const router = useRouter();
  const { ready, signedIn, api, accessToken, workspaceId } = useSession();

  const [meeting, setMeeting] = useState<Meeting | null>(null);
  const [segments, setSegments] = useState<TranscriptSegment[]>([]);
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<'summary' | 'transcript' | 'ask'>('summary');

  const [question, setQuestion] = useState('');
  const [answer, setAnswer] = useState<AskAnswer | null>(null);
  const [asking, setAsking] = useState(false);

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
    const options = { accessToken: accessToken ?? undefined, workspaceId };
    const load = async () => {
      try {
        const [detail, segmentPage] = await Promise.all([
          api.getMeeting(meetingId, options),
          api.listSegments(meetingId, options),
        ]);
        if (cancelled) {
          return;
        }
        setMeeting(detail);
        setSegments(segmentPage.items);
        try {
          const summaryResult = await api.getSummary(meetingId, options);
          if (!cancelled) {
            setSummary(summaryResult);
          }
        } catch {
          if (!cancelled) {
            setSummary(null);
          }
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Could not load the meeting');
        }
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [api, signedIn, accessToken, workspaceId, meetingId]);

  const submitQuestion = useCallback(
    async (event: React.FormEvent) => {
      event.preventDefault();
      if (!workspaceId || !question.trim()) {
        return;
      }
      setAsking(true);
      setAnswer(null);
      try {
        const result = await api.ask(meetingId, question.trim(), {
          accessToken: accessToken ?? undefined,
          workspaceId,
        });
        setAnswer(result);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Could not answer the question');
      } finally {
        setAsking(false);
      }
    },
    [api, meetingId, question, accessToken, workspaceId],
  );

  if (!ready) {
    return <main className="p-8 text-slate-500">Loading...</main>;
  }

  return (
    <main className="mx-auto max-w-3xl px-6 py-10">
      <Link href="/meetings" className="text-sm text-slate-500">
        Back to meetings
      </Link>
      <h1 className="mt-3 text-2xl font-semibold text-ink">{meeting?.title || 'Meeting'}</h1>
      {error ? <p className="mt-4 text-sm text-red-600">{error}</p> : null}

      <nav className="mt-6 flex gap-4 border-b border-slate-200">
        {(['summary', 'transcript', 'ask'] as const).map((item) => (
          <button
            key={item}
            onClick={() => setTab(item)}
            className={`pb-2 text-sm capitalize ${tab === item ? 'border-b-2 border-accent text-ink' : 'text-slate-400'}`}
          >
            {item}
          </button>
        ))}
      </nav>

      {tab === 'summary' ? (
        <section className="mt-6 whitespace-pre-wrap text-slate-700">
          {summary ? summary.content : 'No summary is available yet.'}
        </section>
      ) : null}

      {tab === 'transcript' ? (
        <section className="mt-6 space-y-3">
          {segments.map((segment) => (
            <p key={segment.id} className="text-slate-700">
              <span className="mr-2 text-xs text-slate-400">{timestamp(segment.start_ms)}</span>
              {segment.speaker ? <span className="mr-1 font-medium text-ink">{segment.speaker}:</span> : null}
              {segment.text}
            </p>
          ))}
          {segments.length === 0 ? <p className="text-slate-500">No transcript yet.</p> : null}
        </section>
      ) : null}

      {tab === 'ask' ? (
        <section className="mt-6">
          <form onSubmit={submitQuestion} className="flex gap-2">
            <input
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              placeholder="Ask about this meeting"
              className="flex-1 rounded-md border border-slate-300 px-3 py-2"
            />
            <button
              type="submit"
              disabled={asking}
              className="rounded-md bg-accent px-4 py-2 text-white disabled:opacity-60"
            >
              {asking ? 'Asking...' : 'Ask'}
            </button>
          </form>
          {answer ? (
            <div className="mt-6">
              <p className="whitespace-pre-wrap text-slate-700">{answer.answer}</p>
              {answer.citations.length > 0 ? (
                <ul className="mt-4 space-y-2 border-l-2 border-slate-200 pl-4">
                  {answer.citations.map((citation) => (
                    <li key={citation.segment_id} className="text-sm text-slate-500">
                      <span className="mr-2 text-xs text-slate-400">{timestamp(citation.start_ms)}</span>
                      {citation.text}
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}
        </section>
      ) : null}
    </main>
  );
}
