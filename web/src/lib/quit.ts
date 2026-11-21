// Quitting Ullage from the UI (decision D-079). The server refuses an unconfirmed quit
// while a scan or a deletion runs and says which; the UI then asks before confirming.
import { api, ApiError } from './api';

export interface Work {
  scanning: boolean;
  deleting: boolean;
}

export type QuitOutcome =
  | { kind: 'quitting'; work: Work }
  | { kind: 'confirm'; work: Work };

type Post = (path: string, body: unknown) => Promise<unknown>;

function readWork(payload: unknown): Work {
  const value = (payload ?? {}) as Partial<Work>;
  return { scanning: value.scanning === true, deleting: value.deleting === true };
}

// requestQuit asks the server to quit. Other failures are thrown for the caller's guard.
export async function requestQuit(confirm: boolean, post: Post = api.post):
  Promise<QuitOutcome> {
  try {
    const accepted = await post('/api/quit', confirm ? { confirm: true } : {});
    return { kind: 'quitting', work: readWork(accepted) };
  } catch (failure) {
    if (failure instanceof ApiError && failure.status === 409) {
      return { kind: 'confirm', work: readWork(failure.payload) };
    }
    throw failure;
  }
}

// quitWarning is what the confirmation says will be lost.
export function quitWarning(work: Work): { title: string; body: string; action: string } {
  if (work.deleting) {
    return {
      title: 'A deletion is in progress',
      body: 'Quitting stops it after the item being deleted now and waits for that item, ' +
        'so nothing is left half-moved. Items not reached yet stay where they are. ' +
        (work.scanning ? 'The running scan is cancelled too. ' : '') +
        'The scan results are discarded when Ullage exits.',
      action: 'Stop deleting and quit',
    };
  }
  return {
    title: 'A scan is in progress',
    body: 'Quitting cancels the scan. Nothing on disk is changed, but its progress and ' +
      'any results in this session are discarded when Ullage exits.',
    action: 'Cancel scan and quit',
  };
}
