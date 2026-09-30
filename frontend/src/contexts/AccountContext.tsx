'use client';

import React, { createContext, useContext, useState, useEffect, useCallback, useMemo, ReactNode } from 'react';
import { invoke } from '@tauri-apps/api/core';
import { AccountApi, AccountApiError, WorkspaceSummary, BotJobSummary } from '@/services/accountApi';

export type AccountMode = 'local' | 'signedIn';

export interface AccountContextValue {
  mode: AccountMode;
  loading: boolean;
  workspaceId: string | null;
  workspaceName: string | null;
  workspaces: WorkspaceSummary[];
  credits: number | null;
  apiBaseUrl: string;
  signInWithGoogle: () => Promise<void>;
  switchWorkspace: (workspaceId: string) => Promise<void>;
  refreshBalance: () => Promise<void>;
  sendBot: (meetingUrl: string, botName?: string) => Promise<BotJobSummary>;
  listBotJobs: () => Promise<BotJobSummary[]>;
  signOut: () => Promise<void>;
}

const AccountContext = createContext<AccountContextValue | null>(null);

const DEFAULT_API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL ?? 'http://localhost:8080';

function isTauri(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;
}

async function storeRefreshToken(token: string): Promise<void> {
  if (isTauri()) {
    await invoke('account_store_refresh_token', { token });
  }
}

async function readRefreshToken(): Promise<string | null> {
  if (!isTauri()) {
    return null;
  }
  return (await invoke<string | null>('account_get_refresh_token')) ?? null;
}

async function clearRefreshToken(): Promise<void> {
  if (isTauri()) {
    await invoke('account_clear_refresh_token');
  }
}

export function AccountProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<AccountMode>('local');
  const [loading, setLoading] = useState(true);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const [workspaceName, setWorkspaceName] = useState<string | null>(null);
  const [workspaces, setWorkspaces] = useState<WorkspaceSummary[]>([]);
  const [credits, setCredits] = useState<number | null>(null);

  const api = useMemo(() => new AccountApi({ baseUrl: DEFAULT_API_BASE_URL }), []);

  const loadWorkspaces = useCallback(
    async (token: string) => {
      const { workspaces: list } = await api.listWorkspaces(token);
      setWorkspaces(list);
      if (list.length > 0) {
        setWorkspaceId((current) => current ?? list[0].id);
        setWorkspaceName((current) => current ?? list[0].name);
      }
      return list;
    },
    [api],
  );

  const refreshBalance = useCallback(async () => {
    if (!accessToken || !workspaceId) {
      return;
    }
    try {
      const snapshot = await api.balance(accessToken, workspaceId);
      setCredits(snapshot.credits);
    } catch {
      setCredits(null);
    }
  }, [api, accessToken, workspaceId]);

  const establishSession = useCallback(
    async (tokens: { access_token: string; refresh_token: string }) => {
      setAccessToken(tokens.access_token);
      await storeRefreshToken(tokens.refresh_token);
      setMode('signedIn');
      await loadWorkspaces(tokens.access_token);
    },
    [loadWorkspaces],
  );

  useEffect(() => {
    let cancelled = false;
    const restore = async () => {
      try {
        const refreshToken = await readRefreshToken();
        if (!refreshToken) {
          return;
        }
        const tokens = await api.refresh(refreshToken);
        if (cancelled) {
          return;
        }
        await establishSession(tokens);
      } catch {
        await clearRefreshToken();
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };
    restore().finally(() => {
      if (!cancelled) {
        setLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [api, establishSession]);

  useEffect(() => {
    void refreshBalance();
  }, [refreshBalance]);

  const signInWithGoogle = useCallback(async () => {
    if (!isTauri()) {
      throw new Error('Google sign-in is only available in the desktop app.');
    }
    const tokens = await invoke<{ access_token: string; refresh_token: string }>('account_google_sign_in', {
      apiBaseUrl: DEFAULT_API_BASE_URL,
    });
    await establishSession(tokens);
  }, [establishSession]);

  const switchWorkspace = useCallback(
    async (nextWorkspaceId: string) => {
      const match = workspaces.find((workspace) => workspace.id === nextWorkspaceId);
      if (!match) {
        return;
      }
      setWorkspaceId(match.id);
      setWorkspaceName(match.name);
    },
    [workspaces],
  );

  const signOut = useCallback(async () => {
    await clearRefreshToken();
    setAccessToken(null);
    setWorkspaceId(null);
    setWorkspaceName(null);
    setWorkspaces([]);
    setCredits(null);
    setMode('local');
  }, []);

  const sendBot = useCallback(
    async (meetingUrl: string, botName?: string) => {
      if (!accessToken || !workspaceId) {
        throw new Error('Sign in to send the notetaker to a meeting.');
      }
      return api.sendBot(accessToken, workspaceId, meetingUrl, botName);
    },
    [api, accessToken, workspaceId],
  );

  const listBotJobs = useCallback(async () => {
    if (!accessToken || !workspaceId) {
      return [];
    }
    const { items } = await api.listBotJobs(accessToken, workspaceId);
    return items;
  }, [api, accessToken, workspaceId]);

  const value = useMemo<AccountContextValue>(
    () => ({
      mode,
      loading,
      workspaceId,
      workspaceName,
      workspaces,
      credits,
      apiBaseUrl: DEFAULT_API_BASE_URL,
      signInWithGoogle,
      switchWorkspace,
      refreshBalance,
      sendBot,
      listBotJobs,
      signOut,
    }),
    [mode, loading, workspaceId, workspaceName, workspaces, credits, signInWithGoogle, switchWorkspace, refreshBalance, sendBot, listBotJobs, signOut],
  );

  return <AccountContext.Provider value={value}>{children}</AccountContext.Provider>;
}

export function useAccount(): AccountContextValue {
  const context = useContext(AccountContext);
  if (!context) {
    throw new Error('useAccount must be used within an AccountProvider');
  }
  return context;
}

export { AccountApiError };
