export type AccountState = {
  mode: 'local' | 'signedIn';
  workspaceName?: string;
  credits?: number;
};

export function useAccount(): AccountState {
  return { mode: 'local' };
}
