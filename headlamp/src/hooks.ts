import { useEffect, useMemo, useState } from 'react';
import { DAY, HORIZON, projectWindows } from './schedule';
import type { ChangeWindowData, WindowInterval } from './types';

export function useNow(): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

export interface Projection {
  action: ChangeWindowData;
  windows: WindowInterval[];
  error?: string;
}

export const MAX_PREVIEW_ACTIONS = 250;

export function projectActions(
  actions: ChangeWindowData[],
  from: number,
  until: number
): Projection[] {
  return actions.map((action, index) => projectAction(action, index, from, until));
}

function projectAction(
  action: ChangeWindowData,
  index: number,
  from: number,
  until: number
): Projection {
  try {
    if (index >= MAX_PREVIEW_ACTIONS)
      throw new Error(
        'Calendar previews are limited to 250 windows. Filter the list to preview this window.'
      );
    return { action, windows: projectWindows(action.spec.schedules, from, until) };
  } catch (error) {
    return { action, windows: [], error: error instanceof Error ? error.message : String(error) };
  }
}

// Yield between bounded batches so long calendar ranges remain navigable. Keep
// old results hidden immediately on a selection change and cancel unfinished work.
export function useCalendarProjections(
  actions: ChangeWindowData[],
  from: number,
  until: number
): Projection[] | null {
  const [result, setResult] = useState<{
    actions: ChangeWindowData[];
    from: number;
    until: number;
    items: Projection[];
  } | null>(null);
  useEffect(() => {
    const items: Projection[] = [];
    let timer: ReturnType<typeof setTimeout>;
    let cancelled = false;
    const next = () => {
      if (cancelled) return;
      const started = performance.now();
      while (items.length < actions.length) {
        const index = items.length;
        items.push(projectAction(actions[index], index, from, until));
        if (performance.now() - started >= 8) break;
      }
      if (items.length < actions.length) timer = setTimeout(next, 0);
      else setResult({ actions, from, until, items });
    };
    timer = setTimeout(next, 0);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [actions, from, until]);
  return result?.actions === actions && result.from === from && result.until === until
    ? result.items
    : null;
}

export function useCurrentProjections(actions: ChangeWindowData[], now: number): Projection[] {
  const day = Math.floor(now / DAY) * DAY;
  return useMemo(() => projectActions(actions, day - HORIZON, day + HORIZON + DAY), [actions, day]);
}

export function readyStatus(action: ChangeWindowData): string {
  const condition = action.status?.conditions?.find(item => item.type === 'Ready');
  if (action.status?.observedGeneration !== action.metadata.generation) return 'Reconciling';
  return condition?.status === 'True' ? 'Ready' : condition?.reason || 'Unknown';
}

export function lifecycleStatus(action: ChangeWindowData): string {
  const condition = action.status?.conditions?.find(item => item.type === 'Active');
  if (
    action.status?.observedGeneration !== action.metadata.generation ||
    (condition?.observedGeneration !== undefined &&
      condition.observedGeneration !== action.metadata.generation)
  )
    return 'Reconciling';
  return condition?.reason || 'Not reported';
}
