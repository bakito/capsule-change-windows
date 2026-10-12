import sdkConfig from '@kinvolk/headlamp-plugin/config/vite.config.mjs';
import { mergeConfig } from 'vitest/config';

export default mergeConfig(sdkConfig, {
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    benchmark: { include: ['src/**/*.bench.ts'] },
  },
});
