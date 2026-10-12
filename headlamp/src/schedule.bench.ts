import { bench, describe } from 'vitest';
import { projectWindows } from './schedule';

const from = Date.parse('2026-10-01T00:00:00Z');
const until = Date.parse('2026-11-05T00:00:00Z');
describe('calendar projection', () => {
  for (const policies of [1, 100]) {
    bench(
      `${policies} daily policies`,
      () => {
        for (let i = 0; i < policies; i++) {
          if (
            !projectWindows(
              [{ name: 'daily', cron: '0 19 * * 1-5', duration: '12h', timeZone: 'Europe/Zurich' }],
              from,
              until
            ).length
          )
            throw new Error('No windows');
        }
      },
      { time: 100, iterations: 5, warmupTime: 0 }
    );
  }
  bench(
    'continuous minutely policy',
    () => {
      if (
        projectWindows([{ name: 'continuous', cron: '* * * * *', duration: '1m' }], from, until)
          .length !== 1
      )
        throw new Error('Union is not continuous');
    },
    { time: 100, iterations: 5, warmupTime: 0 }
  );
});
