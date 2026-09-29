
import { check, Update } from '@tauri-apps/plugin-updater';
import { relaunch } from '@tauri-apps/plugin-process';
import { getVersion } from '@tauri-apps/api/app';

export interface UpdateInfo {
  available: boolean;
  currentVersion: string;
  version?: string;
  date?: string;
  body?: string;
  downloadUrl?: string;
}

export interface UpdateProgress {
  downloaded: number;
  total: number;
  percentage: number;
}

export class UpdateService {
  private updateCheckInProgress = false;
  private lastCheckTime: number | null = null;
  private readonly CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;

  async checkForUpdates(force = false): Promise<UpdateInfo> {
    if (this.updateCheckInProgress) {
      throw new Error('Update check already in progress');
    }

    if (!force && this.lastCheckTime) {
      const timeSinceLastCheck = Date.now() - this.lastCheckTime;
      if (timeSinceLastCheck < this.CHECK_INTERVAL_MS) {
        console.log('Skipping update check - checked recently');
        return {
          available: false,
          currentVersion: await getVersion(),
        };
      }
    }

    this.updateCheckInProgress = true;
    this.lastCheckTime = Date.now();

    try {
      const currentVersion = await getVersion();
      const update = await check();

      if (update?.available) {
        return {
          available: true,
          currentVersion,
          version: update.version,
          date: update.date,
          body: update.body,
        };
      }

      return {
        available: false,
        currentVersion,
      };
    } catch (error) {
      console.error('Failed to check for updates:', error);
      throw error;
    } finally {
      this.updateCheckInProgress = false;
    }
  }

  async downloadAndInstall(
    update: Update,
    onProgress?: (progress: UpdateProgress) => void
  ): Promise<void> {
    try {
      await update.download();

      if (onProgress) {
        onProgress({ downloaded: 100, total: 100, percentage: 100 });
      }

      await update.install();
      await relaunch();
    } catch (error) {
      console.error('Failed to download/install update:', error);
      throw error;
    }
  }

  async getCurrentVersion(): Promise<string> {
    return getVersion();
  }

  wasCheckedRecently(): boolean {
    if (!this.lastCheckTime) return false;
    const timeSinceLastCheck = Date.now() - this.lastCheckTime;
    return timeSinceLastCheck < this.CHECK_INTERVAL_MS;
  }
}

export const updateService = new UpdateService();
