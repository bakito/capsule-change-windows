import { describe, expect, it } from 'vitest';
import { calendarDates, calendarDays, calendarRange, shiftPeriod } from './calendar';
import { projectActions } from './hooks';
import { DAY, HORIZON, projectWindows } from './schedule';
import { actionFixture } from './testFixtures';

describe('calendar periods', () => {
  it('starts weeks on Monday and includes complete weeks in a month', () => {
    const week = calendarRange(new Date(2026, 9, 4), 'week');
    expect(week.start).toEqual(new Date(2026, 8, 28));
    expect(week.end).toEqual(new Date(2026, 9, 5));
    const month = calendarRange(new Date(2026, 7, 31), 'month');
    expect(month.start).toEqual(new Date(2026, 6, 27));
    expect(month.end).toEqual(new Date(2026, 8, 7));
    expect(calendarDates(month.start, month.end)).toHaveLength(42);
  });
  it('handles leap years and never rolls a short month into the following month', () => {
    expect(shiftPeriod(new Date(2028, 0, 31), 'month', 1)).toEqual(new Date(2028, 1, 1));
    expect(shiftPeriod(new Date(2028, 0, 31), 'month', -1)).toEqual(new Date(2027, 11, 1));
    expect(shiftPeriod(new Date(2028, 1, 29), 'year', 1)).toEqual(new Date(2029, 0, 1));
    const year = calendarRange(new Date(2028, 4, 1), 'year');
    const dates = calendarDates(year.start, year.end);
    expect(dates).toHaveLength(366);
    expect(dates[59]).toEqual(new Date(2028, 1, 29));
    expect(dates[365]).toEqual(new Date(2028, 11, 31));
  });
  it('advances local calendar days over daylight-saving boundaries', () => {
    const previous = process.env.TZ;
    process.env.TZ = 'Europe/Zurich';
    try {
      const dates = calendarDates(new Date(2026, 2, 28), new Date(2026, 2, 31));
      expect(dates.map(date => date.getHours())).toEqual([0, 0, 0]);
      expect(dates[2].getTime() - dates[1].getTime()).toBe(23 * 3_600_000);
      expect(shiftPeriod(new Date(2026, 9, 19), 'week', 1)).toEqual(new Date(2026, 9, 26));
    } finally {
      if (previous === undefined) delete process.env.TZ;
      else process.env.TZ = previous;
    }
  });
});

describe('calendar event buckets', () => {
  it('includes carry-over windows and excludes their exact end without mutating inputs', () => {
    const dates = calendarDates(new Date(2026, 0, 1), new Date(2026, 0, 4));
    const projections = [
      {
        action: actionFixture(),
        windows: [
          { start: new Date(2025, 11, 31).getTime(), end: dates[1].getTime() },
          { start: dates[1].getTime() + 1000, end: dates[2].getTime() + 1000 },
        ],
      },
    ];
    const before = structuredClone(projections);
    const days = calendarDays(projections, dates, 3);
    expect(days.map(day => day.count)).toEqual([1, 1, 1]);
    expect(days[0].events[0].window.start).toBe(new Date(2025, 11, 31).getTime());
    expect(projections).toEqual(before);
  });
  it('bounds rendered entries while counting all windows and handles empty/error projections', () => {
    const date = new Date(2026, 9, 1);
    const projections = Array.from({ length: 250 }, () => ({
      action: actionFixture(),
      windows: [{ start: date.getTime(), end: date.getTime() + 1000 }],
    }));
    const [day] = calendarDays(projections, [date], 3);
    expect(day.count).toBe(250);
    expect(day.events).toHaveLength(3);
    expect(
      calendarDays([{ action: actionFixture(), windows: [], error: 'invalid' }], [date], 3)[0].count
    ).toBe(0);
    expect(calendarDays([], [], 3)).toEqual([]);
  });
  it('retains the earliest events even when policy names sort differently', () => {
    const date = new Date(2026, 9, 1);
    const projections = [3000, 1000, 2000].map((offset, index) => {
      const action = actionFixture();
      action.metadata.name = `policy-${index}`;
      return {
        action,
        windows: [{ start: date.getTime() + offset, end: date.getTime() + offset + 1000 }],
      };
    });
    const day = calendarDays(projections, [date], 2)[0];
    expect(day.count).toBe(3);
    expect(day.events.map(event => event.action.metadata.name)).toEqual(['policy-1', 'policy-2']);
  });
  it('projects leap-day and annual schedules across the full year with carry-in', () => {
    const range = calendarRange(new Date(2028, 1, 1), 'year');
    const action = actionFixture();
    action.spec.schedules = [
      { name: 'leap', cron: '0 12 29 2 *', timeZone: 'UTC', duration: '1h' },
      { name: 'new-year', cron: '0 23 31 12 *', timeZone: 'UTC', duration: '2h' },
    ];
    const [projection] = projectActions(
      [action],
      range.start.getTime() - HORIZON,
      range.end.getTime() + HORIZON
    );
    expect(projection.error).toBeUndefined();
    expect(
      projection.windows.some(window => window.start === Date.parse('2028-02-29T12:00:00Z'))
    ).toBe(true);
    expect(
      projection.windows.some(window => window.start === Date.parse('2027-12-31T23:00:00Z'))
    ).toBe(true);
  });
  it('retains occurrence and policy caps for the yearly view', () => {
    const action = actionFixture();
    action.spec.schedules = [{ name: 'dense', cron: '*/5 * * * *', duration: '1s' }];
    expect(() => projectWindows(action.spec.schedules, 0, 394 * DAY)).toThrow(/too dense/);
    action.spec.schedules = [{ name: 'continuous', cron: '* * * * *', duration: '1m' }];
    const result = projectActions(
      Array.from({ length: 251 }, () => action),
      0,
      394 * DAY
    );
    expect(result[0].windows).toHaveLength(1);
    expect(result[250].error).toContain('250');
    expect(() => projectWindows(action.spec.schedules, 0, 401 * DAY)).toThrow(/400 days/);
  });
});
