import { describe, expect, it } from 'vitest';
import { currentWindow, DAY, durationMilliseconds, mergeWindows, projectWindows } from './schedule';
import type { Schedule } from './types';

const timestamp = (value: string) => Date.parse(value);
const schedule = (cron: string, duration = '1h', timeZone = 'UTC'): Schedule => ({
  name: 'test',
  cron,
  duration,
  timeZone,
});
const at = (entries: Schedule[], now: string) => {
  const time = timestamp(now);
  return currentWindow(projectWindows(entries, time - 14 * DAY, time + 14 * DAY), time);
};

describe('controller-compatible schedule projections', () => {
  it('merges overlaps and adjacency, with an inclusive start and exclusive end', () => {
    const entries = [
      { ...schedule('0 10 * * *', '2h'), name: 'first' },
      { ...schedule('0 11 * * *', '2h'), name: 'overlap' },
      { ...schedule('0 13 * * *'), name: 'adjacent' },
    ];
    expect(at(entries, '2026-10-01T09:59:59Z')).toBeUndefined();
    expect(at(entries, '2026-10-01T10:00:00Z')).toEqual({
      start: timestamp('2026-10-01T10:00:00Z'),
      end: timestamp('2026-10-01T14:00:00Z'),
    });
    expect(at(entries, '2026-10-01T14:00:00Z')).toBeUndefined();
  });

  it('skips missing DST starts and includes both repeated-hour starts', () => {
    const entries = [schedule('30 2 * * *', '1h', 'Europe/Zurich')];
    expect(at(entries, '2026-03-29T01:45:00Z')).toBeUndefined();
    expect(at(entries, '2026-10-25T00:45:00Z')).toEqual({
      start: timestamp('2026-10-25T00:30:00Z'),
      end: timestamp('2026-10-25T02:30:00Z'),
    });
    expect(at(entries, '2026-10-25T02:30:00Z')).toBeUndefined();
  });

  it('supports half-hour DST transitions without creating nonexistent starts', () => {
    expect(
      at([schedule('15 2 * * *', '10m', 'Australia/Lord_Howe')], '2026-10-03T15:45:00Z')
    ).toBeUndefined();
    expect(
      at([schedule('45 1 * * *', '1h', 'Australia/Lord_Howe')], '2026-04-04T14:50:00Z')?.end
    ).toBe(timestamp('2026-04-04T16:15:00Z'));
  });

  it('uses OR for explicit day-of-month/day-of-week but AND for a star', () => {
    expect(at([schedule('0 10 1 * MON')], '2026-10-05T10:30:00Z')).toBeDefined();
    expect(at([schedule('0 10 1 * MON')], '2026-10-01T10:30:00Z')).toBeDefined();
    expect(at([schedule('0 10 * * MON')], '2026-10-01T10:30:00Z')).toBeUndefined();
    expect(at([schedule('0 10 1-31 * MON')], '2026-10-01T10:30:00Z')).toBeDefined();
    expect(at([schedule('0 10 */2 * MON')], '2026-10-03T10:30:00Z')).toBeDefined();
  });

  it('compresses continuous minutely windows and handles midnight offsets', () => {
    const window = at([schedule('* * * * *', '1m')], '2026-10-01T10:00:00Z');
    expect(window!.end).toBeGreaterThan(timestamp('2026-10-15T10:00:00Z'));
    expect(
      at([schedule('0 0 * * *', '30m', 'Asia/Kathmandu')], '2026-10-01T18:20:00Z')
    ).toBeDefined();
  });

  it('does not mutate input intervals', () => {
    const input = [
      { start: 2, end: 4 },
      { start: 1, end: 3 },
    ];
    expect(mergeWindows(input)).toEqual([{ start: 1, end: 4 }]);
    expect(input).toEqual([
      { start: 2, end: 4 },
      { start: 1, end: 3 },
    ]);
  });

  it.each(['0h', '-1h', '169h', '1d', 'PT1H', '1h rubbish', 'Infinitys', ''])(
    'rejects invalid duration %s',
    value => {
      expect(() => durationMilliseconds(value)).toThrow();
    }
  );
  it('supports fractional and compound Go durations', () => {
    expect(durationMilliseconds('1h30m0.5s')).toBe(5_400_500);
    expect(durationMilliseconds('+1.5h')).toBe(5_400_000);
    expect(durationMilliseconds('168h')).toBe(7 * DAY);
  });
  it.each(['@daily', '0 0 0 * * *', 'CRON_TZ=UTC 0 0 * * *', '70 * * * *'])(
    'rejects unsupported cron %s',
    cron => {
      expect(() => at([schedule(cron)], '2026-10-01T10:00:00Z')).toThrow();
    }
  );
  it('rejects invalid zones and bounds dense or excessive work', () => {
    expect(() =>
      at([schedule('* * * * *', '1h', 'Mars/Olympus')], '2026-10-01T10:00:00Z')
    ).toThrow();
    expect(() => projectWindows([schedule('* * * * *', '1s')], 0, 20 * DAY)).toThrow(/too dense/);
    expect(() => projectWindows([schedule('0 0 * * *')], 0, 401 * DAY)).toThrow(/400 days/);
    expect(() => projectWindows([], 0, DAY)).toThrow();
  });
});
