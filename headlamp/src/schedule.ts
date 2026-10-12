import { Temporal } from '@js-temporal/polyfill';
import { CronExpressionParser } from 'cron-parser';
import type { Schedule, WindowInterval } from './types';

export const DAY = 86_400_000;
export const HORIZON = 14 * DAY;
const MAX_OCCURRENCES = 10_000;

/** Kubernetes metav1.Duration uses Go duration syntax, not ISO-8601 durations. */
export function durationMilliseconds(value: string): number {
  const units: Record<string, number> = {
    h: 3_600_000,
    m: 60_000,
    s: 1000,
    ms: 1,
    us: 0.001,
    µs: 0.001,
    μs: 0.001,
    ns: 0.000001,
  };
  const input = value.startsWith('+') ? value.slice(1) : value;
  const parts = [...input.matchAll(/(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/g)];
  const result = parts.reduce((sum, part) => sum + Number(part[1]) * units[part[2]], 0);
  if (
    parts.map(part => part[0]).join('') !== input ||
    !Number.isFinite(result) ||
    result <= 0 ||
    result > 7 * DAY
  ) {
    throw new Error(`Invalid window duration: ${value}`);
  }
  return result;
}

// Match robfig/cron's star bit: a */N step greater than one clears it. An
// explicit full range is distinct from '*', particularly for DOM/DOW matching.
function hasStar(field: string): boolean {
  return field.split(',').some(part => /^[*?](?:\/0*1)?$/.test(part));
}

function instants(day: Temporal.PlainDate, hour: number, minute: number, zone: string): number[] {
  const wall = day.toPlainDateTime({ hour, minute });
  const results = new Set<number>();
  for (const disambiguation of ['earlier', 'later'] as const) {
    const zoned = wall.toZonedDateTime(zone, { disambiguation });
    // Skip missing local times; include both instances of a repeated hour.
    if (zoned.toPlainDateTime().equals(wall)) results.add(zoned.epochMilliseconds);
  }
  return [...results].sort((a, b) => a - b);
}

export function mergeWindows(intervals: WindowInterval[]): WindowInterval[] {
  const result: WindowInterval[] = [];
  for (const next of [...intervals].sort((a, b) => a.start - b.start)) {
    const last = result[result.length - 1];
    if (last && next.start <= last.end) last.end = Math.max(last.end, next.end);
    else result.push({ ...next });
  }
  return result;
}

/** Enumerate start instants in a bounded range, then form the controller's union. */
export function projectWindows(
  schedules: Schedule[],
  from: number,
  until: number
): WindowInterval[] {
  if (
    !Number.isFinite(from) ||
    !Number.isFinite(until) ||
    until <= from ||
    until - from > 90 * DAY
  ) {
    throw new Error('Calendar range must be positive and at most 90 days.');
  }
  if (!schedules?.length || schedules.length > 16) throw new Error('Expected 1 to 16 schedules.');
  const names = new Set<string>();
  const intervals: WindowInterval[] = [];
  let count = 0;
  for (const schedule of schedules) {
    if (!schedule.name || names.has(schedule.name))
      throw new Error('Schedule names must be unique.');
    names.add(schedule.name);
    const duration = durationMilliseconds(schedule.duration);
    const fields = schedule.cron.trim().split(/\s+/);
    if (fields.length !== 5 || fields.some(field => !/^[\dA-Za-z*?,/\-]+$/.test(field))) {
      throw new Error(`Unsupported cron expression: ${schedule.cron}`);
    }
    const parsed = CronExpressionParser.parse(schedule.cron, { tz: 'UTC' }).fields;
    const zone = schedule.timeZone || 'UTC';
    let day = Temporal.Instant.fromEpochMilliseconds(from - DAY)
      .toZonedDateTimeISO(zone)
      .toPlainDate();
    const lastDay = Temporal.Instant.fromEpochMilliseconds(until + DAY)
      .toZonedDateTimeISO(zone)
      .toPlainDate();
    const everyDOM = parsed.dayOfMonth.values.length === 31;
    const everyDOW = new Set(parsed.dayOfWeek.values.map(value => Number(value) % 7)).size === 7;
    const everyDay =
      hasStar(fields[2]) || hasStar(fields[4]) ? everyDOM && everyDOW : everyDOM || everyDOW;
    if (
      parsed.minute.values.length === 60 &&
      parsed.hour.values.length === 24 &&
      parsed.month.values.length === 12 &&
      everyDay &&
      duration >= 60_000
    ) {
      // A continuous minutely schedule needs only two boundaries, regardless of
      // timezone/DST. Avoid enumerating thousands of equivalent start instants.
      intervals.push({
        start: Math.ceil(from / 60_000) * 60_000,
        end: Math.floor(until / 60_000) * 60_000 + duration,
      });
      continue;
    }
    const push = (start: number, end: number) => {
      if (end <= from || start > until) return;
      if (++count > MAX_OCCURRENCES)
        throw new Error('This schedule is too dense to preview. Narrow the calendar range.');
      intervals.push({ start, end });
    };
    for (; Temporal.PlainDate.compare(day, lastDay) <= 0; day = day.add({ days: 1 })) {
      if (!parsed.month.values.some(value => value === day.month)) continue;
      const dom = parsed.dayOfMonth.values.some(value => value === day.day);
      const dow = parsed.dayOfWeek.values.some(value => value === day.dayOfWeek % 7);
      if (!(hasStar(fields[2]) || hasStar(fields[4]) ? dom && dow : dom || dow)) continue;
      for (const hour of parsed.hour.values) {
        if (parsed.minute.values.length === 60 && duration >= 60_000) {
          const starts = instants(day, hour, 0, zone);
          const ends = instants(day, hour, 59, zone);
          // Collapse ordinary/repeated full hours. Partial DST hours fall through
          // to minute enumeration so a half-hour transition is handled correctly.
          if (
            starts.length &&
            starts.length === ends.length &&
            starts.every((start, i) => ends[i] - start === 59 * 60_000)
          ) {
            starts.forEach((start, i) => push(start, ends[i] + duration));
            continue;
          }
        }
        for (const minute of parsed.minute.values) {
          for (const start of instants(day, hour, minute, zone)) push(start, start + duration);
        }
      }
    }
  }
  return mergeWindows(intervals);
}

export function currentWindow(windows: WindowInterval[], now: number): WindowInterval | undefined {
  return windows.find(window => window.start <= now && now < window.end);
}

export function weekStart(date: Date): Date {
  const result = new Date(date);
  result.setHours(0, 0, 0, 0);
  result.setDate(result.getDate() - ((result.getDay() + 6) % 7));
  return result;
}

export function shiftDays(date: Date, days: number): Date {
  const result = new Date(date);
  result.setDate(result.getDate() + days);
  return result;
}
