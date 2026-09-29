'use client';

import React, { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { useAccount, AccountApiError } from '@/contexts/AccountContext';
import { Loader2 } from 'lucide-react';

interface SignInDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function SignInDialog({ open, onOpenChange }: SignInDialogProps) {
  const { sendCode, verifyCode, apiBaseUrl } = useAccount();
  const [stage, setStage] = useState<'destination' | 'code'>('destination');
  const [destination, setDestination] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setStage('destination');
    setDestination('');
    setCode('');
    setError(null);
    setBusy(false);
  };

  const close = (next: boolean) => {
    if (!next) {
      reset();
    }
    onOpenChange(next);
  };

  const friendlyError = (err: unknown): string => {
    if (err instanceof AccountApiError) {
      return err.message;
    }
    if (err instanceof TypeError) {
      return `Could not reach the server at ${apiBaseUrl}. Check that the backend is running.`;
    }
    return 'Something went wrong. Please try again.';
  };

  const submitDestination = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await sendCode(destination.trim());
      setStage('code');
    } catch (err) {
      setError(friendlyError(err));
    } finally {
      setBusy(false);
    }
  };

  const submitCode = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await verifyCode(destination.trim(), code.trim());
      close(false);
    } catch (err) {
      setError(friendlyError(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Sign in to Afterword</DialogTitle>
          <DialogDescription>
            {stage === 'destination'
              ? 'Enter your email or phone and we will send a one time code.'
              : `Enter the code we sent to ${destination}.`}
          </DialogDescription>
        </DialogHeader>

        {stage === 'destination' ? (
          <form onSubmit={submitDestination} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="signin-destination">Email or phone</Label>
              <Input
                id="signin-destination"
                value={destination}
                onChange={(event) => setDestination(event.target.value)}
                placeholder="you@example.com"
                autoFocus
                required
              />
            </div>
            {error ? <p className="text-sm text-red-600">{error}</p> : null}
            <Button type="submit" disabled={busy || !destination.trim()} className="w-full">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Send code'}
            </Button>
          </form>
        ) : (
          <form onSubmit={submitCode} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="signin-code">Verification code</Label>
              <Input
                id="signin-code"
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder="123456"
                inputMode="numeric"
                autoFocus
                required
              />
            </div>
            {error ? <p className="text-sm text-red-600">{error}</p> : null}
            <Button type="submit" disabled={busy || !code.trim()} className="w-full">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Verify and continue'}
            </Button>
            <button
              type="button"
              className="w-full text-sm text-gray-500 hover:text-gray-700"
              onClick={() => {
                setStage('destination');
                setError(null);
              }}
            >
              Use a different address
            </button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
