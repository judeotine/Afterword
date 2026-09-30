'use client';

import React, { createContext, useCallback, useContext, useEffect, useMemo, useState, ReactNode } from 'react';
import { ApiClient } from '@/lib/api';

const REFRESH_KEY = 'afterword.refresh';

interface Workspace {
  id: string;
  name: string;
  role: string;
}

interface SessionValue {
  ready: boolean;
  signedIn: boolean;
  accessToken: string | null;
  workspaceId: string | null;
  workspaces: Workspace[];
  api: ApiClient;
  signInWithGoogle: () => void;
  completeTokenSignIn: (accessToken: string, refreshToken: string) => Promise<void>;
  selectWorkspace: (workspaceId: string) => void;
  signOut: () => void;
}

const SessionContext = createContext<SessionValue | null>(null);

function readStored(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  return window.localStorage.getItem(REFRESH_KEY);
}

function writeStored(token: string | null): void {
  if (typeof window === 'undefined') {
    return;
  }
  if (token) {
    window.localStorage.setItem(REFRESH_KEY, token);
  } else {
    window.localStorage.removeItem(REFRESH_KEY);
  }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const api = useMemo(() => new ApiClient(), []);
  const [ready, setReady] = useState(false);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);

  const hydrate = useCallback(
    async (token: string) => {
      setAccessToken(token);
      const { workspaces: list } = await api.listWorkspaces({ accessToken: token });
      setWorkspaces(list);
      setWorkspaceId((current) => current ?? (list[0]?.id ?? null));
    },
    [api],
  );

  useEffect(() => {
    let cancelled = false;
    const restore = async () => {
      const refreshToken = readStored();
      if (!refreshToken) {
        setReady(true);
        return;
      }
      try {
        const tokens = await api.refresh(refreshToken);
        if (cancelled) {
          return;
        }
        writeStored(tokens.refresh_token);
        await hydrate(tokens.access_token);
      } catch {
        writeStored(null);
      } finally {
        if (!cancelled) {
          setReady(true);
        }
      }
    };
    void restore();
    return () => {
      cancelled = true;
    };
  }, [api, hydrate]);

  const signInWithGoogle = useCallback(() => {
    if (typeof window === 'undefined') {
      return;
    }
    window.location.href = api.googleStartUrl('/sign-in');
  }, [api]);

  const completeTokenSignIn = useCallback(
    async (token: string, refreshToken: string) => {
      writeStored(refreshToken);
      await hydrate(token);
    },
    [hydrate],
  );

  const selectWorkspace = useCallback((next: string) => {
    setWorkspaceId(next);
  }, []);

  const signOut = useCallback(() => {
    writeStored(null);
    setAccessToken(null);
    setWorkspaces([]);
    setWorkspaceId(null);
  }, []);

  const value = useMemo<SessionValue>(
    () => ({
      ready,
      signedIn: Boolean(accessToken),
      accessToken,
      workspaceId,
      workspaces,
      api,
      signInWithGoogle,
      completeTokenSignIn,
      selectWorkspace,
      signOut,
    }),
    [ready, accessToken, workspaceId, workspaces, api, signInWithGoogle, completeTokenSignIn, selectWorkspace, signOut],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionValue {
  const context = useContext(SessionContext);
  if (!context) {
    throw new Error('useSession must be used within a SessionProvider');
  }
  return context;
}
