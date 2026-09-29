
export interface BetaFeatures {
  importAndRetranscribe: boolean;
}

export const DEFAULT_BETA_FEATURES: BetaFeatures = {
  importAndRetranscribe: true, // Default: enabled
};

export const BETA_FEATURE_NAMES: Record<keyof BetaFeatures, string> = {
  importAndRetranscribe: 'Import Audio & Retranscribe',
};

export const BETA_FEATURE_DESCRIPTIONS: Record<keyof BetaFeatures, string> = {
  importAndRetranscribe: 'Import audio files to transcribe or retranscribe existing meetings with different language settings.',
};

export type BetaFeatureKey = keyof BetaFeatures;

export function loadBetaFeatures(): BetaFeatures {
  if (typeof window === 'undefined') {
    return { ...DEFAULT_BETA_FEATURES };
  }

  try {
    const saved = localStorage.getItem('betaFeatures');
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<BetaFeatures>;
      return { ...DEFAULT_BETA_FEATURES, ...parsed };
    }
  } catch (error) {
    console.error('[BetaFeatures] Failed to load from localStorage:', error);
  }

  return { ...DEFAULT_BETA_FEATURES };
}

export function saveBetaFeatures(features: BetaFeatures): void {
  if (typeof window === 'undefined') return;

  try {
    localStorage.setItem('betaFeatures', JSON.stringify(features));
  } catch (error) {
    console.error('[BetaFeatures] Failed to save to localStorage:', error);
  }
}
