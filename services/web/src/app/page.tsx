'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';

export default function SignInPage() {
  const { signedIn, signInWithGoogle, completeTokenSignIn } = useSession();
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    const params = new URLSearchParams(window.location.search);
    if (params.get('error') === 'google') {
      setError('Google sign-in did not complete. Please try again.');
      window.history.replaceState(null, '', '/sign-in');
      return;
    }

    const hash = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : '';
    if (!hash) {
      return;
    }
    const fragment = new URLSearchParams(hash);
    const accessToken = fragment.get('access_token');
    const refreshToken = fragment.get('refresh_token');
    if (accessToken && refreshToken) {
      setBusy(true);
      window.history.replaceState(null, '', '/sign-in');
      completeTokenSignIn(accessToken, refreshToken)
        .then(() => router.replace('/meetings'))
        .catch(() => {
          setError('Could not complete sign-in. Please try again.');
          setBusy(false);
        });
    }
  }, [completeTokenSignIn, router]);

  useEffect(() => {
    if (signedIn) {
      router.replace('/meetings');
    }
  }, [signedIn, router]);

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col justify-center px-6">
      <h1 className="text-3xl font-semibold text-ink">Afterword</h1>
      <p className="mt-2 text-slate-500">Sign in to reach your meeting library.</p>

      <div className="mt-8">
        <button
          type="button"
          onClick={() => {
            setError(null);
            setBusy(true);
            signInWithGoogle();
          }}
          disabled={busy}
          className="flex w-full items-center justify-center gap-3 rounded-md border border-slate-300 bg-white px-4 py-2.5 font-medium text-ink shadow-sm transition hover:bg-slate-50 disabled:opacity-60"
        >
          <svg width="18" height="18" viewBox="0 0 18 18" aria-hidden="true">
            <path fill="#4285F4" d="M17.64 9.2c0-.637-.057-1.251-.164-1.84H9v3.481h4.844a4.14 4.14 0 0 1-1.796 2.716v2.259h2.908c1.702-1.567 2.684-3.875 2.684-6.615z" />
            <path fill="#34A853" d="M9 18c2.43 0 4.467-.806 5.956-2.184l-2.908-2.259c-.806.54-1.837.86-3.048.86-2.344 0-4.328-1.583-5.036-3.711H.957v2.332A8.997 8.997 0 0 0 9 18z" />
            <path fill="#FBBC05" d="M3.964 10.706A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.706V4.962H.957A8.997 8.997 0 0 0 0 9c0 1.452.348 2.827.957 4.038l3.007-2.332z" />
            <path fill="#EA4335" d="M9 3.583c1.321 0 2.508.454 3.44 1.345l2.582-2.58C13.463.891 11.426 0 9 0A8.997 8.997 0 0 0 .957 4.962L3.964 7.294C4.672 5.166 6.656 3.583 9 3.583z" />
          </svg>
          {busy ? 'Connecting...' : 'Continue with Google'}
        </button>
      </div>

      {error ? <p className="mt-4 text-sm text-red-600">{error}</p> : null}

      <p className="mt-8 text-xs text-slate-400">
        By continuing you agree to the Afterword Terms of Service and Privacy Policy.
      </p>
    </main>
  );
}
