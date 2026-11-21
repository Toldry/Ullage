import { describe, expect, it } from 'vitest';
import {
  deselectAll, groupSelection, pruneSelection, restKey, selectAll, selectedTotals,
  selectGroups, selectionCategories, selectionState, toggleCategory,
} from './results';
import type { ResultCategory, ResultsView } from './types';

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

  it('selects all selectable categories of a group and skips the rest', () => {
    const open = category('ai:1', { moreItems: 5, moreBytes: 500 });
    const recipe = category('recipe:npm', { source: 'recipe', confidence: 'recipe' });
    const kept = category('ai:2', { selectable: false });
    const locked = category('ai:3', { needsExpand: true, selectable: false });
    const group = [open, recipe, kept, locked];
    const selected = selectAll(group, new Set(['other|id:1']));
    expect(selectionState(open, selected)).toBe('on');
    expect(selectionState(recipe, selected)).toBe('on');
    expect(selectionState(kept, selected)).toBe('off');
    expect(selectionState(locked, selected)).toBe('off');
    expect(selected.has('other|id:1')).toBe(true);
    expect(groupSelection(group, selected)).toEqual({ selectable: 2, state: 'on' });
  });

  it('reports a group as partly selected and clears only that group', () => {
    const first = category('ai:1');
    const second = category('ai:2');
    const outside = category('recipe:npm', { source: 'recipe' });
    const selection = selectAll([first, outside], new Set());
    expect(groupSelection([first, second], selection))
      .toEqual({ selectable: 2, state: 'partial' });
    const cleared = deselectAll([first, second], selection);
    expect(groupSelection([first, second], cleared).state).toBe('off');
    expect(selectionState(outside, cleared)).toBe('on');
  });

  it('counts a group with nothing selectable as empty', () => {
    const kept = category('ai:2', { selectable: false });
    expect(groupSelection([kept], new Set())).toEqual({ selectable: 0, state: 'off' });
    expect(selectAll([kept], new Set()).size).toBe(0);
  });

  it('offers source buttons only when both sources are listed', () => {
    const ai = category('ai:1');
    const recipe = category('recipe:npm', { source: 'recipe' });
    expect(selectGroups([ai, recipe]).map((group) => group.key))
      .toEqual(['shown', 'recipe', 'ai']);
    expect(selectGroups([ai]).map((group) => group.key)).toEqual(['shown']);
  });
});

describe('allow deleting against AI recommendation (D-078)', () => {
  const view = (kept: ResultCategory[]) => ({ categories: [category('ai:1')], kept }) as
    unknown as ResultsView;

  it('only offers keep categories for selection while allowed', () => {
    const kept = category('ai:2', { selectable: false, bruteForce: { allowed: false } });
    expect(selectionCategories(view([kept])).map((entry) => entry.key)).toEqual(['ai:1']);
    const allowed = { ...kept, selectable: true, keepOverridden: true };
    expect(selectionCategories(view([allowed])).map((entry) => entry.key))
      .toEqual(['ai:1', 'ai:2']);
  });

  it('drops keep categories from the selection once it is turned off', () => {
    const allowed = category('ai:2', { keepOverridden: true });
    const selection = toggleCategory(allowed, new Set());
    const keptAgain = { ...allowed, keepOverridden: false, selectable: false };
    expect(pruneSelection(selectionCategories(view([allowed])), selection).size).toBe(2);
    expect(pruneSelection(selectionCategories(view([keptAgain])), selection).size).toBe(0);
  });

  it('lets select all skip keep categories unless allowed', () => {
    const kept = category('ai:2', { selectable: false });
    expect(selectAll([category('ai:1'), kept], new Set()).has('ai:2|id:1')).toBe(false);
  });
});
