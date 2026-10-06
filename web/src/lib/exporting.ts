import { api } from './api';
import type { ExportView, PasteResult } from './types';

export const exportApi = {
  get: () => api.get<ExportView>('/api/export'),
  rebuild: () => api.post<ExportView>('/api/export/rebuild'),
  paste: (batch: number, text: string) => api.post<PasteResult>('/api/paste', { batch, text }),
};

export const checkLabels = {
  session: 'Session code',
  complete: 'Complete reply',
  format: 'Format and values',
  ids: 'Item IDs',
  duplicates: 'No duplicates',
} as const;
