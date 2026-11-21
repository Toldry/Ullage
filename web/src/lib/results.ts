import { api } from './api';
import type { ResultCategory, ResultsView } from './types';

export const resultsApi = {
  get: () => api.get<ResultsView>('/api/results'),
  expand: (key: string) => api.post<ResultsView>('/api/results/expand', { key }),
  unchanged: (days: number) => api.post<ResultsView>('/api/results/unchanged', { days }),
};

// The age filter's choices: list only items whose newest change is this old (D-071).
export const unchangedChoices: { days: number; label: string; since: string }[] = [
  { days: 0, label: 'any time', since: '' },
  { days: 30, label: '1 month', since: 'the last month' },
  { days: 182, label: '6 months', since: 'the last 6 months' },
  { days: 365, label: '1 year', since: 'the last year' },
  { days: 730, label: '2 years', since: 'the last 2 years' },
  { days: 1825, label: '5 years', since: 'the last 5 years' },
];

// Selection holds item keys. A category with more items than the list shows is selected
// as a whole through its "rest" key, so hidden items are never selected one by one.
export type Selection = ReadonlySet<string>;

export const restKey = (category: ResultCategory) => `${category.key}/rest`;

export function categoryKeys(category: ResultCategory): string[] {
  const keys = category.items.map((item) => `${category.key}|${item.key}`);
  return category.moreItems > 0 ? [...keys, restKey(category)] : keys;
}

export const itemKey = (category: ResultCategory, key: string) => `${category.key}|${key}`;

export function selectionState(category: ResultCategory, selection: Selection):
  'on' | 'off' | 'partial' {
  const keys = categoryKeys(category);
  const selected = keys.filter((key) => selection.has(key)).length;
  return selected === 0 ? 'off' : selected === keys.length ? 'on' : 'partial';
}

export function toggleCategory(category: ResultCategory, selection: Selection): Set<string> {
  const next = new Set(selection);
  const keys = categoryKeys(category);
  const allOn = keys.every((key) => next.has(key));
  for (const key of keys) {
    if (allOn) next.delete(key);
    else next.add(key);
  }
  return next;
}

// selectAll adds every item of the selectable categories among the given ones. Categories
// that cannot be selected (kept by the AI, locked, unexplained, managed cleanup only) are
// skipped, so a group's "Select all" never reaches them (D-077).
export function selectAll(categories: ResultCategory[], selection: Selection): Set<string> {
  const next = new Set(selection);
  for (const category of categories) {
    if (category.selectable) categoryKeys(category).forEach((key) => next.add(key));
  }
  return next;
}

// deselectAll removes every item of the given categories from the selection.
export function deselectAll(categories: ResultCategory[], selection: Selection): Set<string> {
  const next = new Set(selection);
  for (const category of categories) categoryKeys(category).forEach((key) => next.delete(key));
  return next;
}

// groupSelection sums up a group of categories for its "Select all" button: how many can be
// selected, and whether all of those, some, or none are selected now.
export function groupSelection(categories: ResultCategory[], selection: Selection):
  { selectable: number; state: 'on' | 'off' | 'partial' } {
  const states = categories.filter((category) => category.selectable)
    .map((category) => selectionState(category, selection));
  const state = states.length === 0 || states.every((entry) => entry === 'off') ? 'off'
    : states.every((entry) => entry === 'on') ? 'on' : 'partial';
  return { selectable: states.length, state };
}

export interface SelectGroup {
  key: string;
  // Words after "Select all" and "Deselect", e.g. "recipes".
  noun: string;
  categories: ResultCategory[];
}

// The groups offered a "Select all" button: everything listed, then each source. The source
// buttons are left out when one source holds every listed row, since they would repeat the
// first button.
export function selectGroups(rows: ResultCategory[]): SelectGroup[] {
  const recipes = rows.filter((category) => category.source === 'recipe');
  const ai = rows.filter((category) => category.source === 'ai');
  const groups: SelectGroup[] = [{ key: 'shown', noun: '', categories: rows }];
  if (recipes.length > 0 && ai.length > 0) {
    groups.push({ key: 'recipe', noun: 'recipes', categories: recipes },
      { key: 'ai', noun: 'AI', categories: ai });
  }
  return groups;
}

export function selectedTotals(categories: ResultCategory[], selection: Selection) {
  let items = 0;
  let bytes = 0;
  for (const category of categories) {
    for (const item of category.items) {
      if (selection.has(itemKey(category, item.key))) {
        items += 1;
        bytes += item.bytes;
      }
    }
    if (selection.has(restKey(category))) {
      items += category.moreItems;
      bytes += category.moreBytes;
    }
  }
  return { items, bytes };
}

// pruneSelection drops keys of categories that are gone or can no longer be selected,
// for example after a batch was imported again.
export function pruneSelection(categories: ResultCategory[], selection: Selection):
  Set<string> {
  const allowed = new Set<string>();
  for (const category of categories) {
    if (category.selectable) categoryKeys(category).forEach((key) => allowed.add(key));
  }
  return new Set([...selection].filter((key) => allowed.has(key)));
}
