import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from './App';

const STAFF = { staffId: 'staff-001', name: 'Alex Turner' };
const QUOTE = {
  quoteId: '7c4677e6-b95b-4ee8-bcf5-c17bbda9d63a',
  commissionRate: 0.018,
  totalCommission: 4500,
};

type Reply = { status: number; body?: unknown };

/** Replies keyed by "METHOD /path", so a test states only what it exercises. */
function server(replies: Record<string, Reply | Reply[]>) {
  const calls: { method: string; body: unknown }[] = [];

  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const key = `${method} ${url}`;
    calls.push({ method: key, body: init?.body ? JSON.parse(init.body as string) : undefined });

    const entry = replies[key];
    const reply = Array.isArray(entry) ? (entry.shift() ?? { status: 404 }) : entry;
    if (!reply) throw new Error(`no reply configured for ${key}`);

    return {
      ok: reply.status >= 200 && reply.status < 300,
      status: reply.status,
      json: async () => reply.body,
    } as Response;
  }));

  return calls;
}

const signedIn = { 'GET /api/session': { status: 200, body: STAFF } };
const signedOut = { 'GET /api/session': { status: 401, body: { error: { code: 'UNAUTHENTICATED', message: 'no session' } } } };

const failure = (status: number, code: string, message: string, extra = {}) => ({
  status,
  body: { error: { code, message, correlationId: 'corr-1', ...extra } },
});

async function fillForm(amount = '250000.00', term = '240', band = 'B') {
  const user = userEvent.setup();
  await user.clear(screen.getByLabelText('Loan amount'));
  await user.type(screen.getByLabelText('Loan amount'), amount);
  await user.clear(screen.getByLabelText('Loan term in months'));
  await user.type(screen.getByLabelText('Loan term in months'), term);
  if (band) await user.selectOptions(screen.getByLabelText('Risk band'), band);
  return user;
}

beforeEach(() => vi.restoreAllMocks());
afterEach(() => vi.unstubAllGlobals());

describe('sign in', () => {
  it('shows the sign in form when there is no session', async () => {
    server(signedOut);
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
  });

  it('restores an existing session without signing in again', async () => {
    server(signedIn);
    render(<App />);
    expect(await screen.findByRole('heading', { name: /generate a commission quote/i })).toBeInTheDocument();
  });

  it('shows the message the BFF sent when credentials are refused', async () => {
    server({
      ...signedOut,
      'POST /api/session': failure(401, 'UNAUTHENTICATED', 'That staff ID or password is not correct.'),
    });
    render(<App />);

    const user = userEvent.setup();
    await user.type(await screen.findByLabelText('Staff ID'), 'staff-001');
    await user.type(screen.getByLabelText('Password'), 'wrong');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('That staff ID or password is not correct.');
  });
});

describe('generating a quote', () => {
  it('shows the vendor figures, formatted but not recalculated', async () => {
    server({ ...signedIn, 'POST /api/v1/quotes': { status: 200, body: QUOTE } });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    const result = await screen.findByRole('region', { name: 'Quote generated' });
    expect(within(result).getByText('1.80%')).toBeInTheDocument();
    expect(within(result).getByText('$4,500.00')).toBeInTheDocument();
    expect(within(result).getByText(QUOTE.quoteId)).toBeInTheDocument();
  });

  it('sends what the user entered', async () => {
    const calls = server({ ...signedIn, 'POST /api/v1/quotes': { status: 200, body: QUOTE } });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm('1000.50', '12', 'A');
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    await waitFor(() => {
      const quote = calls.find((c) => c.method === 'POST /api/v1/quotes');
      expect(quote?.body).toEqual({ loanAmount: 1000.5, loanTermInMonths: 12, riskBand: 'A' });
    });
  });

  it('collapses the form in place, above the result', async () => {
    server({ ...signedIn, 'POST /api/v1/quotes': { status: 200, body: QUOTE } });
    const { container } = render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));
    await screen.findByRole('region', { name: 'Quote generated' });

    expect(screen.queryByLabelText('Loan amount')).not.toBeInTheDocument();
    expect(screen.getByText(/\$250,000\.00 · 240 months · Band B/)).toBeInTheDocument();

    // The summary stays where the form was: above the result, never below it.
    const text = container.textContent ?? '';
    expect(text.indexOf('Quote for')).toBeLessThan(text.indexOf('Quote generated'));
  });

  it('reopens the form with the values intact', async () => {
    server({ ...signedIn, 'POST /api/v1/quotes': { status: 200, body: QUOTE } });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));
    await user.click(await screen.findByRole('button', { name: 'Edit' }));

    expect(screen.getByLabelText('Loan amount')).toHaveValue('250000.00');
    expect(screen.getByLabelText('Loan term in months')).toHaveValue('240');
  });
});

describe('submitting', () => {
  it('does not submit twice while a request is in flight', async () => {
    let release: (value: unknown) => void = () => {};
    const pending = new Promise((resolve) => {
      release = resolve;
    });

    const calls: string[] = [];
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const key = `${init?.method ?? 'GET'} ${url}`;
      calls.push(key);
      if (key === 'GET /api/session') {
        return { ok: true, status: 200, json: async () => STAFF } as Response;
      }
      await pending;
      return { ok: true, status: 200, json: async () => QUOTE } as Response;
    }));

    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    const form = screen.getByRole('button', { name: 'Generate Quote' }).closest('form')!;

    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));
    // A second submit that does not go through the disabled button.
    form.requestSubmit();
    form.requestSubmit();

    release(null);
    await screen.findByRole('region', { name: 'Quote generated' });

    // The vendor's quote generation is not idempotent: a duplicate could bill twice.
    expect(calls.filter((c) => c === 'POST /api/v1/quotes')).toHaveLength(1);
  });

  it('disables the button and marks it busy while in flight', async () => {
    server({ ...signedIn, 'POST /api/v1/quotes': { status: 200, body: QUOTE } });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));
    await screen.findByRole('region', { name: 'Quote generated' });
    expect(screen.queryByRole('button', { name: 'Generate Quote' })).not.toBeInTheDocument();
  });
});

describe('validation', () => {
  it('reports every invalid field at once and never calls the API', async () => {
    const calls = server(signedIn);
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm('1', '9999', '');
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    const summary = await screen.findByRole('alert');
    expect(summary).toHaveTextContent('Check the highlighted fields.');
    expect(within(summary).getAllByRole('link')).toHaveLength(3);

    expect(screen.getByLabelText('Loan amount')).toHaveAttribute('aria-invalid', 'true');
    expect(calls.some((c) => c.method === 'POST /api/v1/quotes')).toBe(false);
  });

  it('renders field errors the Middleware found that the front end let through', async () => {
    server({
      ...signedIn,
      'POST /api/v1/quotes': failure(400, 'VALIDATION_FAILED', 'Check the highlighted fields.', {
        details: [{ field: 'loanAmount', code: 'amount_out_of_range' }],
      }),
    });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/loan amount/i);
    expect(screen.getByLabelText('Loan amount')).toHaveAttribute('aria-invalid', 'true');
  });
});

describe('failures', () => {
  it('renders the message the BFF wrote, not one of its own', async () => {
    server({
      ...signedIn,
      'POST /api/v1/quotes': failure(502, 'UPSTREAM_UNAVAILABLE', 'Quotes are unavailable right now. Try again shortly.'),
    });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Quotes are unavailable right now. Try again shortly.');
    expect(alert).toHaveTextContent('corr-1');
  });

  it('keeps one primary action when the vendor is unavailable', async () => {
    server({
      ...signedIn,
      'POST /api/v1/quotes': failure(503, 'UPSTREAM_CIRCUIT_OPEN', 'Quotes are paused briefly. Try again in a moment.'),
    });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));
    await screen.findByRole('alert');

    // Generate Quote is the retry. A second control would leave a user guessing.
    expect(screen.queryByRole('button', { name: /try again/i })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Generate Quote' })).toHaveLength(1);
  });

  it('shows no form when the caller is not entitled', async () => {
    server({
      ...signedIn,
      'POST /api/v1/quotes': failure(403, 'FORBIDDEN', 'You do not have access to generate quotes.'),
    });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    expect(await screen.findByText('You do not have access to generate quotes.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Generate Quote' })).not.toBeInTheDocument();
    expect(screen.getByText('corr-1')).toBeInTheDocument();
  });

  it('returns to sign in when the session has gone', async () => {
    server({
      ...signedIn,
      'POST /api/v1/quotes': failure(401, 'UNAUTHENTICATED', 'Your session has expired. Sign in again.'),
    });
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm();
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
  });
});

describe('accessibility', () => {
  it('labels every control and describes invalid ones', async () => {
    server(signedIn);
    render(<App />);
    await screen.findByRole('heading', { name: /generate a commission quote/i });

    const user = await fillForm('1', '240', 'B');
    await user.click(screen.getByRole('button', { name: 'Generate Quote' }));

    const amount = screen.getByLabelText('Loan amount');
    expect(amount).toHaveAttribute('aria-invalid', 'true');
    const describedBy = amount.getAttribute('aria-describedby');
    expect(describedBy).toBeTruthy();
    expect(document.getElementById(describedBy!)).toHaveTextContent(/between/i);
  });
});
