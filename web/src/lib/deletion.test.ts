import { describe, expect, it } from 'vitest';
import { categoryWarnings, errorLog, splitPath } from './deletion';
import type { DeletionItem } from './types';

describe('deletion helpers', () => {
  it('splits paths, keeping loose contents with their folder name', () => {
    expect(splitPath('~/projects/web/node_modules')).toEqual(['~/projects/web/',
      'node_modules']);
    expect(splitPath('~/Clutter/*')).toEqual(['~/', 'Clutter/*']);
    expect(splitPath('setup.iso')).toEqual(['', 'setup.iso']);
  });

  it('logs only items that were not removed', () => {
    const item = (status: DeletionItem['status'], reason?: string): DeletionItem => ({
      key: status, category: 'ai:1', categoryName: 'x', path: `~/${status}`, kind: 'folder',
      bytes: 1, status, reason,
    });
    const log = errorLog([item('recycled'), item('failed', 'in use'), item('changed', 'x')]);
    expect(log.split('\n')).toEqual(['Failed\t~/failed\tin use',
      'Changed since scan\t~/changed\tx']);
  });

  it('lists each ready category warning once', () => {
    const item = (key: string, category: string, status: DeletionItem['status'],
      warning?: string): DeletionItem => ({ key, category, categoryName: category,
      path: `~/${key}`, kind: 'folder', bytes: 1, status, warning });
    expect(categoryWarnings([
      item('a', 'recipe:rustup-toolchains', 'ready', 'rustup still lists them'),
      item('b', 'recipe:rustup-toolchains', 'ready', 'rustup still lists them'),
      item('c', 'recipe:ollama-models', 'changed', 'pull again'),
      item('d', 'ai:1', 'ready'),
    ])).toEqual([{ name: 'recipe:rustup-toolchains', warning: 'rustup still lists them' }]);
  });
});
