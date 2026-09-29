'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';
import { ApiError } from '@/lib/api';

export default function SignInPage() {
  const { sendCode, verifyCode, signedIn } = useSession();
  const router = useRouter();
  const [destination, setDestination] = useState('');
  const [code, setCode] = useState('');
  const [stage, setStage] = useState<'destination' | 'code'>('destination');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (signedIn) {
    router.replace('/meetings');
  }

  const submitDestination = async (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await sendCode(destination.trim());
      setStage('code');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not send the code');
    } finally {
      setBusy(false);
    }
  };

  const submitCode = async (event: React.FormEvent) => {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await verifyCode(destination.trim(), code.trim());
      router.replace('/meetings');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not verify the code');
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6">
      <h1 className="text-3xl font-semibold text-ink">Afterword</h1>
      <p className="mt-2 text-slate-500">Sign in to reach your meeting library.</p>
      {stage === 'destination' ? (
        <form onSubmit={submitDestination} className="mt-8 space-y-4">
          <label className="block text-sm font-medium text-slate-700" htmlFor="destination">
            Email or phone
          </label>
          <input
            id="destination"
            className="w-full rounded-md border border-slate-300 px-3 py-2"
            value={destination}
            onChange={(event) => setDestination(event.target.value)}
            placeholder="you@example.com"
            required
          />
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-md bg-accent px-4 py-2 font-medium text-white disabled:opacity-60"
          >
            {busy ? 'Sending...' : 'Send code'}
          </button>
        </form>
      ) : (
        <form onSubmit={submitCode} className="mt-8 space-y-4">
          <label className="block text-sm font-medium text-slate-700" htmlFor="code">
            Verification code
          </label>
          <input
            id="code"
            className="w-full rounded-md border border-slate-300 px-3 py-2 tracking-widest"
            value={code}
            onChange={(event) => setCode(event.target.value)}
            placeholder="123456"
            inputMode="numeric"
            required
          />
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-md bg-accent px-4 py-2 font-medium text-white disabled:opacity-60"
          >
            {busy ? 'Verifying...' : 'Verify and continue'}
          </button>
          <button
            type="button"
            className="w-full text-sm text-slate-500"
            onClick={() => setStage('destination')}
          >
            Use a different address
          </button>
        </form>
      )}
      {error ? <p className="mt-4 text-sm text-red-600">{error}</p> : null}
    </main>
  );
}
