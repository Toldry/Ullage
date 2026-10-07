import { describe, expect, it } from 'vitest';
import {
  pruneSelection, restKey, roundsOf, selectedTotals, selectionState, toggleCategory,
} from './results';
import type { ExportView, ResultCategory } from './types';

function category(key: string, overrides: Partial<ResultCategory> = {}): ResultCategory {
  return {
    key, name: key, source: 'ai', confidence: 'high', regenerable: 'yes', bytes: 30,
    files: 3, itemCount: 2, root: '~/', moreItems: 0, moreBytes: 0,
    explanationState: 'received', bruteForce: { allowed: true }, needsExpand: false,
    needsExplanation: false, cloudSyncItems: 0, notLocalBytes: 0,
    selectable: true,
    items: [
      { key: 'id:1', kind: 'folder', path: '~/a', bytes: 10, files: 1, modified: 0 },
      { key: 'id:2', kind: 'folder', path: '~/b', bytes: 20, files: 2, modified: 0 },
    ],
    ...overrides,
  };
}

describe('result selection', () => {
  it('toggles a whole category, including items beyond the listed ones', () => {
    const big = category('ai:1', { moreItems: 5, moreBytes: 500 });
    const selected = toggleCategory(big, new Set());
    expect(selectionState(big, selected)).toBe('on');
    expect(selected.has(restKey(big))).toBe(true);
    expect(selectedTotals([big], selected)).toEqual({ items: 7, bytes: 530 });
    const partial = new Set(selected);
    partial.delete('ai:1|id:1');
    expect(selectionState(big, partial)).toBe('partial');
    expect(selectionState(big, toggleCategory(big, selected))).toBe('off');
  });

  it('drops selections of categories that are gone or locked', () => {
    const open = category('ai:1');
    const selection = new Set([...toggleCategory(open, new Set()), 'ai:9|id:7']);
    expect([...pruneSelection([open], selection)].sort()).toEqual(['ai:1|id:1', 'ai:1|id:2']);
    const locked = category('ai:1', { needsExpand: true, selectable: false });
    expect(pruneSelection([locked], selection).size).toBe(0);
  });
});

describe('rounds', () => {
  it('appends the explanation prompt after the batches', () => {
    const view = {
      code: 'ABCDEF', items: 1, representedBytes: 0, unknownBytes: 0, minimumItemBytes: 0,
      stale: false,
      batches: [{ number: 1, firstId: 1, lastId: 1, lines: 1, items: 1, text: 'a',
        characters: 1, tokenEstimate: 1, imported: true, categoryCount: 1,
        classifiedItems: 1 }],
      explanation: { round: 2, text: 'b', categories: 3, characters: 1, tokenEstimate: 1 },
    } satisfies ExportView;
    const rounds = roundsOf(view);
    expect(rounds.map((round) => [round.number, round.kind])).toEqual([[1, 'batch'],
      [2, 'explain']]);
    expect(rounds[1].items).toBe(3);
  });
});
