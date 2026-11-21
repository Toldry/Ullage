import { describe, expect, it } from 'vitest';
import { ApiError } from './api';
import { quitWarning, requestQuit } from './quit';

describe('requestQuit', () => {
  it('quits at once when nothing runs', async () => {
    const sent: unknown[] = [];
    const outcome = await requestQuit(false, async (_path, body) => {
      sent.push(body);
      return { ok: true, scanning: false, deleting: false };
    });
    expect(outcome).toEqual({ kind: 'quitting', work: { scanning: false, deleting: false } });
    expect(sent).toEqual([{}]);
  });

  it('asks for confirmation when the server reports work in progress', async () => {
    const outcome = await requestQuit(false, async () => {
      throw new ApiError('busy', 409, { ok: false, scanning: false, deleting: true });
    });
    expect(outcome).toEqual({ kind: 'confirm', work: { scanning: false, deleting: true } });
  });

  it('sends the confirmation flag the second time', async () => {
    const sent: unknown[] = [];
    const outcome = await requestQuit(true, async (path, body) => {
      sent.push([path, body]);
      return { ok: true, scanning: true, deleting: false };
    });
    expect(outcome.kind).toBe('quitting');
    expect(sent).toEqual([['/api/quit', { confirm: true }]]);
  });

  it('passes other failures on', async () => {
    await expect(requestQuit(false, async () => {
      throw new ApiError('bad', 400);
    })).rejects.toBeInstanceOf(ApiError);
  });
});

describe('quitWarning', () => {
  it('names what is lost', () => {
    expect(quitWarning({ scanning: true, deleting: false }).action)
      .toBe('Cancel scan and quit');
    const deleting = quitWarning({ scanning: true, deleting: true });
    expect(deleting.action).toBe('Stop deleting and quit');
    expect(deleting.body).toContain('scan is cancelled too');
  });
});

describe('api', () => {
  it('reports a server that does not answer as unreachable', async () => {
    const { api, ServerUnreachableError } = await import('./api');
    const realFetch = globalThis.fetch;
    globalThis.fetch = (() => Promise.reject(new TypeError('Failed to fetch'))) as typeof fetch;
    try {
      await expect(api.post('/api/heartbeat')).rejects.toBeInstanceOf(ServerUnreachableError);
    } finally {
      globalThis.fetch = realFetch;
    }
  });
});
