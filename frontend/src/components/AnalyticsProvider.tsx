'use client';

import React, { useEffect, ReactNode, useRef, useState, createContext } from 'react';
import Analytics from '@/lib/analytics';
import { load } from '@tauri-apps/plugin-store';

const ANALYTICS_DEFAULT_OFF_MIGRATION_KEY = 'analyticsDefaultOffMigrationV1';

interface AnalyticsProviderProps {
  children: ReactNode;
}

interface AnalyticsContextType {
  isAnalyticsOptedIn: boolean;
  setIsAnalyticsOptedIn: (optedIn: boolean) => void;
}

export const AnalyticsContext = createContext<AnalyticsContextType>({
  isAnalyticsOptedIn: false,
  setIsAnalyticsOptedIn: () => { },
});

export default function AnalyticsProvider({ children }: AnalyticsProviderProps) {
  const [isAnalyticsOptedIn, setIsAnalyticsOptedIn] = useState(false);
  const initialized = useRef(false);

  useEffect(() => {
    if (initialized.current) {
      return;
    }

    const initAnalytics = async () => {
      const store = await load('analytics.json', {
        autoSave: false,
        defaults: {
          analyticsOptedIn: false
        }
      });

      if (!(await store.has('analyticsOptedIn'))) {
        await store.set('analyticsOptedIn', false);
      }

      let analyticsOptedIn = await store.get<boolean>('analyticsOptedIn');
      if (!(await store.has(ANALYTICS_DEFAULT_OFF_MIGRATION_KEY))) {
        analyticsOptedIn = false;
        await store.set('analyticsOptedIn', false);
        await store.set(ANALYTICS_DEFAULT_OFF_MIGRATION_KEY, true);
        await store.save();
      } else if (analyticsOptedIn !== true) {
        analyticsOptedIn = false;
      }

      setIsAnalyticsOptedIn(analyticsOptedIn as boolean);
      if (analyticsOptedIn) {
        await initAnalytics2();
      }
    }

    const initAnalytics2 = async () => {

      initialized.current = true;

      const userId = await Analytics.getPersistentUserId();

      await Analytics.init();

      const deviceInfo = await Analytics.getDeviceInfo();

      const store = await load('analytics.json', {
        autoSave: false,
        defaults: {
          analyticsOptedIn: false
        }
      });
      await store.set('platform', deviceInfo.platform);
      await store.set('os_version', deviceInfo.os_version);
      await store.set('architecture', deviceInfo.architecture);

      if (!(await store.has('first_launch_date'))) {
        await store.set('first_launch_date', new Date().toISOString());
      }

      await store.save();

      await Analytics.identify(userId, {
        app_version: '0.4.0',
        platform: deviceInfo.platform,
        os_version: deviceInfo.os_version,
        architecture: deviceInfo.architecture,
        first_seen: new Date().toISOString(),
      });

      const sessionId = await Analytics.startSession(userId);
      if (sessionId) {
        await Analytics.trackSessionStarted(sessionId);
      }

      await Analytics.checkAndTrackFirstLaunch();

      await Analytics.trackAppStarted();

      await Analytics.checkAndTrackDailyUsage();

      const handleBeforeUnload = async () => {
        if (sessionId) {
          await Analytics.trackSessionEnded(sessionId);
        }
        await Analytics.cleanup();
      };

      window.addEventListener('beforeunload', handleBeforeUnload);

      return () => {
        window.removeEventListener('beforeunload', handleBeforeUnload);
        if (sessionId) {
          Analytics.trackSessionEnded(sessionId);
        }
        Analytics.cleanup();
      };

    };

    initAnalytics().catch(console.error);
  }, []);

  useEffect(() => {
    if (!isAnalyticsOptedIn) {
      initialized.current = false;
    }
  }, [isAnalyticsOptedIn]);

  return <AnalyticsContext.Provider value={{ isAnalyticsOptedIn, setIsAnalyticsOptedIn }}>{children}</AnalyticsContext.Provider>;
}
