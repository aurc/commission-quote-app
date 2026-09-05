// The BFF is the only thing this app talks to. It owns the session, mints the
// bearer for the Middleware, and writes the wording shown to a user, so an
// error's `message` is rendered as it arrives.

export type Staff = { staffId: string; name: string };

export type Quote = {
  quoteId: string;
  commissionRate: number;
  totalCommission: number;
};

export type ApiError = {
  code: string;
  message: string;
  details?: { field: string; code: string }[];
  correlationId?: string;
};

export class RequestFailed extends Error {
  constructor(
    readonly status: number,
    readonly error: ApiError,
  ) {
    super(error.message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      ...init,
    });
  } catch {
    throw new RequestFailed(0, {
      code: 'NETWORK',
      message: 'Cannot reach the server. Check your connection and try again.',
    });
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const body = await response.json().catch(() => null);

  if (!response.ok) {
    throw new RequestFailed(
      response.status,
      body?.error ?? { code: 'INTERNAL', message: 'Something went wrong. Try again.' },
    );
  }
  return body as T;
}

export const api = {
  currentSession: () => request<Staff>('/api/session'),

  signIn: (staffId: string, password: string) =>
    request<Staff>('/api/session', {
      method: 'POST',
      body: JSON.stringify({ staffId, password }),
    }),

  signOut: () => request<void>('/api/session', { method: 'DELETE' }),

  quote: (body: unknown) =>
    request<Quote>('/api/v1/quotes', { method: 'POST', body: JSON.stringify(body) }),
};
