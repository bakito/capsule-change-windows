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
  return actions.map((action, index) => {
    try {
      // Bound browser work even if an administrator installs many policies.
      if (index >= MAX_PREVIEW_ACTIONS)
        throw new Error(
          'Calendar previews are limited to 250 windows. Filter the list to preview this window.'
        );
      return { action, windows: projectWindows(action.spec.schedules, from, until) };
    } catch (error) {
      return { action, windows: [], error: error instanceof Error ? error.message : String(error) };
    }
  });
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
