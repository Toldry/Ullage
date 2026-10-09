import { api } from './api';
import type { DeletionItem, DeletionPlan, DeletionRun, DeletionStatus } from './types';

export const deletionApi = {
  prepare: (keys: string[]) => api.post<DeletionPlan>('/api/delete/prepare', { keys }),
  execute: (planId: string, permanent: boolean, confirmation: string) =>
    api.post<DeletionRun>('/api/delete/execute', { planId, permanent, confirmation }),
  plan: () => api.get<DeletionPlan>('/api/delete/plan'),
  progress: () => api.get<DeletionRun>('/api/delete/progress'),
  stop: () => api.post<DeletionRun>('/api/delete/stop'),
};

export type Tone = 'ok' | 'bad' | 'warn' | 'none';

export const statusLabels: Record<DeletionStatus, { label: string; tone: Tone }> = {
  pending: { label: 'Waiting', tone: 'none' },
  ready: { label: 'Ready', tone: 'ok' },
  changed: { label: 'Changed since scan', tone: 'bad' },
  missing: { label: 'Missing', tone: 'none' },
  protected: { label: 'Protected', tone: 'bad' },
  rejected: { label: 'Refused', tone: 'bad' },
  recycled: { label: 'Moved to bin', tone: 'ok' },
  deleted: { label: 'Deleted', tone: 'ok' },
  failed: { label: 'Failed', tone: 'bad' },
  partial: { label: 'Partly removed', tone: 'warn' },
  skipped: { label: 'Skipped', tone: 'none' },
};

// Statuses that mean the item is still on disk because Ullage left it alone on purpose.
const leftInPlace: DeletionStatus[] = ['changed', 'protected', 'rejected', 'skipped'];

// outcomeLabel is the row label once a run has finished (or is running): it leads with
// what happened to the item, so a blocked item never reads as if it might be gone.
export function outcomeLabel(status: DeletionStatus): { label: string; tone: Tone } {
  switch (status) {
    case 'changed': return { label: 'Not deleted · changed', tone: 'warn' };
    case 'protected': return { label: 'Not deleted · protected', tone: 'warn' };
    case 'rejected': return { label: 'Not deleted · refused', tone: 'warn' };
    case 'skipped': return { label: 'Not deleted · stopped', tone: 'none' };
    case 'missing': return { label: 'Already gone', tone: 'none' };
    case 'failed': return { label: 'Failed · still there', tone: 'bad' };
    default: return statusLabels[status];
  }
}

// leftInPlaceCount counts items Ullage deliberately did not touch.
export function leftInPlaceCount(items: DeletionItem[]): number {
  return items.filter((item) => leftInPlace.includes(item.status)).length;
}

// splitPath separates the folder (shown dimmed) from the name, keeping "/*" with the name.
export function splitPath(path: string): [string, string] {
  const loose = path.endsWith('/*');
  const bare = loose ? path.slice(0, -2) : path;
  const cut = bare.lastIndexOf('/');
  const name = bare.slice(cut + 1) + (loose ? '/*' : '');
  return [cut >= 0 ? bare.slice(0, cut + 1) : '', name];
}

// errorLog is the text "Copy error log" puts on the clipboard: what failed and why.
export function errorLog(items: DeletionItem[]): string {
  return items
    .filter((item) => ['failed', 'partial', 'changed', 'protected', 'rejected', 'missing']
      .includes(item.status))
    .map((item) => `${statusLabels[item.status].label}\t${item.path || item.key}\t` +
      `${item.reason ?? ''}`)
    .join('\n');
}

export const retryableStatuses: DeletionStatus[] = ['failed', 'partial'];

// categoryWarnings lists each ready category's brute-force warning once, for the
// confirmation, instead of repeating a long text on every item.
export function categoryWarnings(items: DeletionItem[]): { name: string; warning: string }[] {
  const seen = new Map<string, { name: string; warning: string }>();
  for (const item of items) {
    if (item.status === 'ready' && item.warning && !seen.has(item.category)) {
      seen.set(item.category, { name: item.categoryName, warning: item.warning });
    }
  }
  return [...seen.values()];
}
