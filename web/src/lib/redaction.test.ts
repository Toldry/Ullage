import { describe, expect, it } from 'vitest';
import { isRedacted, placeholderSegments, reasonText, treatmentText } from './redaction';
import type { RedactionItem } from './types';

const item = (overrides: Partial<RedactionItem>): RedactionItem => ({
  id: 1, name: 'x', path: '~/x', kind: 'folder', bytes: 0, files: 0, hasChildren: false,
  decision: 'visible', source: '', rendered: '~/x', keepWellKnown: false, ...overrides,
});

describe('placeholderSegments', () => {
  it('marks placeholders for highlighting', () => {
    expect(placeholderSegments('C:/Users/~/<R3>/build/<R12>.pdf')).toEqual([
      { text: 'C:/Users/~/', placeholder: false },
      { text: '<R3>', placeholder: true },
      { text: '/build/', placeholder: false },
      { text: '<R12>', placeholder: true },
      { text: '.pdf', placeholder: false },
    ]);
  });
});

describe('treatment and reason labels', () => {
  it('describes each state', () => {
    expect(isRedacted(item({ decision: 'home' }))).toBe(false);
    expect(isRedacted(item({ decision: 'hidden' }))).toBe(true);
    expect(treatmentText(item({ decision: 'subtree', kind: 'file' }))).toBe('Whole name');
    expect(treatmentText(item({ decision: 'subtree' }))).toBe('Whole subtree');
    expect(reasonText(item({ source: 'session' }))).toBe('you · this session');
  });
});
