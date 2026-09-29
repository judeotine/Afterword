import { useEffect, useState } from 'react';
import { updateService, UpdateInfo } from '@/services/updateService';
import { showUpdateNotification } from '@/components/UpdateNotification';

interface UseUpdateCheckOptions {
  checkOnMount?: boolean;
  showNotification?: boolean;
  onUpdateAvailable?: (info: UpdateInfo) => void;
}

export function useUpdateCheck(options: UseUpdateCheckOptions = {}) {
  const {
    checkOnMount = true,
    showNotification = true,
    onUpdateAvailable,
  } = options;

  const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null);
  const [isChecking, setIsChecking] = useState(false);

  const checkForUpdates = async (force = false) => {
    if (!force && updateService.wasCheckedRecently()) {
      return;
    }

    setIsChecking(true);
    try {
      const info = await updateService.checkForUpdates(force);
      setUpdateInfo(info);

      if (info.available) {
        if (onUpdateAvailable) {
          onUpdateAvailable(info);
        } else if (showNotification) {
          showUpdateNotification(info, () => {
            // This will be handled by the component that uses this hook
          });
        }
      }
    } catch (error) {
      console.error('Failed to check for updates:', error);
      // Silently fail on startup checks to avoid disrupting user experience
    } finally {
      setIsChecking(false);
    }
  };

  useEffect(() => {
    if (checkOnMount) {
      const timer = setTimeout(() => {
        checkForUpdates(false);
      }, 2000);

      return () => clearTimeout(timer);
    }
  }, [checkOnMount]);

  return {
    updateInfo,
    isChecking,
    checkForUpdates,
  };
}
