'use client';

import React, { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { useAccount, AccountApiError } from '@/contexts/AccountContext';
import { Loader2 } from 'lucide-react';
import { toast } from 'sonner';

interface SendBotDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

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

export function SendBotDialog({ open, onOpenChange }: SendBotDialogProps) {
  const { sendBot, mode, apiBaseUrl } = useAccount();
  const [url, setUrl] = useState('');
  const [botName, setBotName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const platform = detectPlatform(url);

  const close = (next: boolean) => {
    if (!next) {
      setUrl('');
      setBotName('');
      setError(null);
      setBusy(false);
    }
    onOpenChange(next);
  };

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await sendBot(url.trim(), botName.trim() || undefined);
      toast.success('Notetaker is on its way to the meeting');
      close(false);
    } catch (err) {
      if (err instanceof AccountApiError && err.status === 403) {
        setError('Only a workspace admin can send the notetaker to a meeting.');
      } else if (err instanceof AccountApiError && err.status === 402) {
        setError('This workspace does not have enough credits. Top up to send the notetaker.');
      } else if (err instanceof TypeError) {
        setError(`Could not reach the server at ${apiBaseUrl}. Check that the backend is running.`);
      } else {
        setError(err instanceof Error ? err.message : 'Could not send the notetaker');
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Send the notetaker to a meeting</DialogTitle>
          <DialogDescription>
            Paste a Google Meet, Zoom or Teams link and the Afterword notetaker will join and record it.
          </DialogDescription>
        </DialogHeader>

        {mode === 'local' ? (
          <p className="text-sm text-amber-600">Sign in first to send the notetaker to meetings.</p>
        ) : (
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="bot-url">Meeting link</Label>
              <Input
                id="bot-url"
                value={url}
                onChange={(event) => setUrl(event.target.value)}
                placeholder="https://meet.google.com/abc-defg-hij"
                autoFocus
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
              <Label htmlFor="bot-name">Notetaker name (optional)</Label>
              <Input
                id="bot-name"
                value={botName}
                onChange={(event) => setBotName(event.target.value)}
                placeholder="Afterword Notetaker"
              />
            </div>
            {error ? <p className="text-sm text-red-600">{error}</p> : null}
            <Button type="submit" disabled={busy || !url.trim() || !platform} className="w-full">
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Send notetaker'}
            </Button>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
