// Thin client for the local API. Every request carries the per-launch token in a header;
// the token never appears in a URL.
export class SessionExpiredError extends Error {}

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

function readToken(): string {
  const meta = document.querySelector('meta[name="ullage-token"]');
  return meta?.getAttribute('content') ?? '';
}

const token = readToken();

async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: {
      'X-Ullage-Token': token,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (response.status === 401 || response.status === 403 || response.status === 421) {
    throw new SessionExpiredError('session rejected');
  }
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new ApiError((payload as { error?: string }).error ?? response.statusText,
      response.status);
  }
  return payload as T;
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body: unknown = {}) => request<T>('POST', path, body),
};
