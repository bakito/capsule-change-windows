import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useCalendarProjections } from './hooks';
import { DAY } from './schedule';
import { actionFixture } from './testFixtures';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
  let elapsed = 0;
  vi.spyOn(performance, 'now').mockImplementation(() => (elapsed += 10));
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

const from = Date.parse('2026-10-01T00:00:00Z');
const until = from + 365 * DAY;
const actions = [actionFixture(), actionFixture(), actionFixture()];

describe('calendar background batches', () => {
  it('yields between policies and cancels stale selection work', () => {
    const { result, rerender } = renderHook(
      ({ selected }) => useCalendarProjections(selected, from, until),
      { initialProps: { selected: actions } }
    );
    expect(result.current).toBeNull();
    act(() => vi.advanceTimersToNextTimer());
    expect(result.current).toBeNull();
    const selected = [actionFixture()];
    selected[0].metadata.name = 'replacement';
    rerender({ selected });
    act(() => vi.runAllTimers());
    expect(result.current?.map(item => item.action.metadata.name)).toEqual(['replacement']);
    expect(result.current?.[0].windows.length).toBeGreaterThan(300);
    expect(vi.getTimerCount()).toBe(0);
  });
  it('hides previous results when the range changes and cancels on unmount', () => {
    const { result, rerender, unmount } = renderHook(
      ({ start, end }) => useCalendarProjections(actions, start, end),
      { initialProps: { start: from, end: until } }
    );
    act(() => vi.runAllTimers());
    expect(result.current).toHaveLength(3);
    rerender({ start: from + DAY, end: until + DAY });
    expect(result.current).toBeNull();
    act(() => vi.advanceTimersToNextTimer());
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
  it('keeps the global policy cap across batches and exposes malformed schedules', () => {
    const selected = Array.from({ length: 251 }, () => {
      const action = actionFixture();
      action.spec.schedules = [{ name: 'continuous', cron: '* * * * *', duration: '1m' }];
      return action;
    });
    selected[1].spec.schedules[0].cron = 'invalid';
    const { result } = renderHook(() => useCalendarProjections(selected, from, until));
    act(() => vi.runAllTimers());
    expect(result.current?.[0].windows).toHaveLength(1);
    expect(result.current?.[1].error).toContain('Unsupported cron');
    expect(result.current?.[249].windows).toHaveLength(1);
    expect(result.current?.[250].error).toContain('250');
  });
});
