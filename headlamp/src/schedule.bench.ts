import { bench, describe } from 'vitest';
import { calendarDates, calendarDays, calendarRange, type CalendarView } from './calendar';
import { projectActions } from './hooks';
import { HORIZON, projectWindows } from './schedule';
import { actionFixture } from './testFixtures';

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

describe('calendar views', () => {
  for (const view of ['week', 'month', 'year'] as CalendarView[]) {
    const range = calendarRange(new Date(2028, 9, 1), view);
    const dates = calendarDates(range.start, range.end);
    for (const policies of [1, 100]) {
      const actions = Array.from({ length: policies }, (_, index) => {
        const action = actionFixture();
        action.metadata.name = `window-${index}`;
        action.spec.schedules = [
          {
            name: 'daily',
            cron: `${index % 60} ${10 + Math.floor(index / 60)} * * *`,
            duration: '2h',
            timeZone: 'Europe/Zurich',
          },
        ];
        return action;
      });
      bench(
        `${view}: ${policies} distinct schedules`,
        () => {
          const projections = projectActions(
            actions,
            range.start.getTime() - HORIZON,
            range.end.getTime() + HORIZON
          );
          const days = calendarDays(projections, dates, view === 'week' ? 100 : 3);
          if (projections.some(item => item.error) || days.some(day => day.count !== policies))
            throw new Error('Incomplete preview');
        },
        { time: 100, iterations: 2, warmupTime: 0 }
      );
    }
  }
});
