import type { Projection } from './hooks';
import { shiftDays, weekStart } from './schedule';
import type { ChangeWindowData, WindowInterval } from './types';

export type CalendarView = 'week' | 'month' | 'year';
export const calendarLabels: Record<CalendarView, string> = {
  week: 'Weekly',
  month: 'Monthly',
  year: 'Yearly',
};

export function calendarRange(date: Date, view: CalendarView): { start: Date; end: Date } {
  if (view === 'week') {
    const start = weekStart(date);
    return { start, end: shiftDays(start, 7) };
  }
  const year = date.getFullYear();
  if (view === 'year') return { start: new Date(year, 0, 1), end: new Date(year + 1, 0, 1) };
  const start = weekStart(new Date(year, date.getMonth(), 1));
  const nextMonth = new Date(year, date.getMonth() + 1, 1);
  const end = weekStart(nextMonth);
  return { start, end: end < nextMonth ? shiftDays(end, 7) : end };
}

export function shiftPeriod(date: Date, view: CalendarView, amount: number): Date {
  if (view === 'week') return shiftDays(date, amount * 7);
  if (view === 'month') return new Date(date.getFullYear(), date.getMonth() + amount, 1);
  return new Date(date.getFullYear() + amount, 0, 1);
}

export function calendarDates(start: Date, end: Date): Date[] {
  const days: Date[] = [];
  for (let date = start; date < end; date = shiftDays(date, 1)) days.push(date);
  return days;
}

export interface CalendarEvent {
  action: ChangeWindowData;
  window: WindowInterval;
}
export interface CalendarDay {
  date: Date;
  count: number;
  events: CalendarEvent[];
}

// Projections contain sorted, merged intervals. Advance a cursor per policy so
// a year does not repeatedly scan every occurrence for every day. Retain only the
// visible entries in each cell; counts still include every overlapping interval.
export function calendarDays(
  projections: Projection[],
  dates: Date[],
  visibleLimit: number
): CalendarDay[] {
  const cursors = projections.map(() => 0);
  return dates.map(date => {
    const start = date.getTime();
    const end = shiftDays(date, 1).getTime();
    let count = 0;
    const events: CalendarEvent[] = [];
    projections.forEach(({ action, windows }, index) => {
      while (cursors[index] < windows.length && windows[cursors[index]].end <= start)
        cursors[index]++;
      for (let i = cursors[index]; i < windows.length && windows[i].start < end; i++) {
        count++;
        const event = { action, window: windows[i] };
        const compare = (other: CalendarEvent) =>
          event.window.start - other.window.start ||
          event.action.metadata.name.localeCompare(other.action.metadata.name);
        if (
          visibleLimit <= 0 ||
          (events.length === visibleLimit && compare(events[events.length - 1]) >= 0)
        )
          continue;
        let low = 0;
        let high = events.length;
        while (low < high) {
          const middle = (low + high) >>> 1;
          if (compare(events[middle]) < 0) high = middle;
          else low = middle + 1;
        }
        events.splice(low, 0, event);
        if (events.length > visibleLimit) events.pop();
      }
    });
    return { date, count, events };
  });
}
