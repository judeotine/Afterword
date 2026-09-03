/** Zoom adapter placeholder — URLs are recognised, joining is Phase 4 work. */
import type { Page } from 'playwright';
import { NotImplementedError } from '../errors.js';
import type { JoinOptions, MeetingPlatform, PlatformName } from './types.js';

export class ZoomPlatform implements MeetingPlatform {
  readonly name: PlatformName = 'zoom';

  async join(_page: Page, _opts: JoinOptions): Promise<void> {
    throw new NotImplementedError('zoom');
  }

  async announceConsent(_page: Page, _text: string): Promise<void> {
    throw new NotImplementedError('zoom');
  }

  async participantCount(_page: Page): Promise<number> {
    throw new NotImplementedError('zoom');
  }

  async isMeetingOver(_page: Page): Promise<boolean> {
    throw new NotImplementedError('zoom');
  }

  async leave(_page: Page): Promise<void> {
    throw new NotImplementedError('zoom');
  }
}
