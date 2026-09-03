import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['test/**/*.test.ts'],
    environment: 'node',
    // PRIVACY_URL is required, and src/config.ts validates the environment at
    // import time, so every test file that imports it needs one.
    env: {
      PRIVACY_URL: 'https://example.test/privacy',
    },
    testTimeout: 120_000,
    hookTimeout: 120_000,
  },
});
