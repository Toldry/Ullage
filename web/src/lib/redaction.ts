import { api } from './api';
import type {
  RedactionChildren,
  RedactionDetail,
  RedactionItem,
  RedactionSummary,
  RuleDraft,
  RuleType,
  RuleView,
} from './types';

export const redactionApi = {
  summary: () => api.get<RedactionSummary>('/api/redaction/summary'),
  children: (code: string, parent: number) =>
    api.get<RedactionChildren>(
      `/api/redaction/children?code=${encodeURIComponent(code)}&parent=${parent}`),
  detail: (code: string, id: number) =>
    api.get<RedactionDetail>(`/api/redaction/item?code=${encodeURIComponent(code)}&id=${id}`),
  override: (code: string, id: number, treatment: string, keepWellKnown: boolean) =>
    api.post<RedactionDetail>('/api/redaction/override',
      { sessionCode: code, id, treatment, keepWellKnown }),
  rules: () => api.get<RuleView[]>('/api/rules'),
  saveRule: (draft: RuleDraft) => api.post<RuleView>('/api/rules/save', draft),
  deleteRule: (id: string) => api.post('/api/rules/delete', { id }),
  resetRules: () => api.post<{ movedTo: string }>('/api/rules/reset', { confirm: 'reset' }),
};

export const ruleTypeLabels: Record<RuleType, string> = {
  exact: 'Exact path',
  subtree: 'Subtree',
  glob: 'Glob',
  name: 'Name anywhere',
};

export const ruleTypeHelp: Record<RuleType, string> = {
  exact: 'One file or folder at exactly this path. Follows it if it is renamed or moved.',
  subtree: 'A folder and everything inside it.',
  glob: '* within one path segment, ** across segments. ~ is your profile.',
  name: 'A file or folder name at any depth, on any drive. * and ? are wildcards.',
};

export const treatmentLabels = {
  subtree: 'Whole subtree',
  'keep-extension': 'Name, keep extension',
} as const;

export function isRedacted(item: RedactionItem): boolean {
  return item.decision !== 'visible' && item.decision !== 'home';
}

export function treatmentText(item: RedactionItem): string {
  switch (item.decision) {
    case 'subtree':
      return item.kind === 'file' ? 'Whole name' : 'Whole subtree';
    case 'keep-extension':
      return item.kind === 'file' ? 'Name, keep ext.' : 'Name only';
    case 'hidden':
      return 'Inside a redacted folder';
    case 'home':
      return 'Shown as ~';
    default:
      return '';
  }
}

export function reasonText(item: RedactionItem): string {
  switch (item.source) {
    case 'session':
      return 'you · this session';
    case 'always':
      return 'always: account, machine name or SID';
    case 'ancestor':
      return 'parent is redacted';
    default:
      return '';
  }
}

// Splits a rendered path so placeholders can be highlighted.
export function placeholderSegments(rendered: string): { text: string; placeholder: boolean }[] {
  return rendered
    .split(/(<R\d+>)/)
    .filter((text) => text !== '')
    .map((text) => ({ text, placeholder: /^<R\d+>$/.test(text) }));
}
