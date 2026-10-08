import { api } from './api';
import type { ExportView, ResultCategory, ResultsView } from './types';

export const resultsApi = {
  get: () => api.get<ResultsView>('/api/results'),
  requestExplanation: (number: number, queued: boolean) =>
    api.post<ResultsView>('/api/results/explain', { number, queued }),
  expand: (key: string) => api.post<ResultsView>('/api/results/expand', { key }),
};

// One step of the Export and Paste screens: a round A batch, an imported explanation
// round kept for review, or the explanation prompt waiting to be sent. Numbers run on
// from the batches, so step N is always rounds[N - 1].
export interface Round {
  number: number;
  kind: 'batch' | 'explain';
  text: string;
  items: number;
  characters: number;
  tokenEstimate: number;
  imported: boolean;
  categoryCount: number;
  classifiedItems: number;
  reply?: string;
  importedAt?: string;
  stillQueued?: number;
}

export function roundsOf(view: ExportView): Round[] {
  const rounds: Round[] = view.batches.map((batch) => ({ ...batch, kind: 'batch' as const }));
  const explanations = [...(view.explanationRounds ?? []),
    ...(view.explanation ? [view.explanation] : [])];
  for (const explanation of explanations) {
    rounds.push({ number: explanation.round, kind: 'explain', text: explanation.text,
      items: explanation.categories, characters: explanation.characters,
      tokenEstimate: explanation.tokenEstimate, imported: explanation.imported,
      categoryCount: explanation.imported ? explanation.explained : explanation.categories,
      classifiedItems: 0, reply: explanation.reply, importedAt: explanation.importedAt,
      stillQueued: explanation.stillQueued });
  }
  return rounds;
}

// roundTitle names a step for the stepper: "Batch 2 of 3" or "Explanations · round 4".
export function roundTitle(round: Round, batchCount: number): string {
  return round.kind === 'batch' ? `Batch ${round.number} of ${batchCount}`
    : `Explanations · round ${round.number}`;
}

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
