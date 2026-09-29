import type { Page } from 'playwright';
import { NotImplementedError } from '../errors.js';
import type { JoinOptions, MeetingPlatform, PlatformName } from './types.js';

export class TeamsPlatform implements MeetingPlatform {
  readonly name: PlatformName = 'teams';

  async join(_page: Page, _opts: JoinOptions): Promise<void> {
    throw new NotImplementedError('teams');
  }

  async announceConsent(_page: Page, _text: string): Promise<void> {
    throw new NotImplementedError('teams');
  }

  async participantCount(_page: Page): Promise<number> {
    throw new NotImplementedError('teams');
  }

  async isMeetingOver(_page: Page): Promise<boolean> {
    throw new NotImplementedError('teams');
  }

  async leave(_page: Page): Promise<void> {
    throw new NotImplementedError('teams');
  }
}
