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
