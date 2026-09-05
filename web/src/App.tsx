import { useEffect, useState } from 'react';
import { api, RequestFailed, type Quote, type Staff } from './api';
import { validate, type FieldError, type QuoteInput } from './validation';
import { Reference } from './components/Banner';
import { QuoteForm } from './components/QuoteForm';
import { QuoteResult } from './components/QuoteResult';
import { QuoteSummary } from './components/QuoteSummary';
import { SignIn } from './components/SignIn';
import { LockIcon, Mark } from './components/icons';

const EMPTY: QuoteInput = { loanAmount: '', loanTermInMonths: '', riskBand: '' };

export function App() {
  const [staff, setStaff] = useState<Staff | null>(null);
  const [loadingSession, setLoadingSession] = useState(true);

  useEffect(() => {
    api
      .currentSession()
      .then(setStaff)
      .catch(() => setStaff(null))
      .finally(() => setLoadingSession(false));
  }, []);

  return (
    <div className="app">
      <header className="bar">
        <div className="bar__brand">
          <Mark />
          <span>Commission Quote</span>
        </div>
        {staff && (
          <div className="bar__staff">
            <span className="bar__staff-name">{staff.name}</span>
            <button
              className="btn btn--on-primary"
              type="button"
              onClick={() => api.signOut().finally(() => setStaff(null))}
            >
              Sign out
            </button>
          </div>
        )}
      </header>

      <main className="page">
        <div className={`column${staff ? '' : ' column--narrow'}`}>
          {loadingSession ? null : staff ? (
            <Quoting staff={staff} onSessionLost={() => setStaff(null)} />
          ) : (
            <SignIn onSignedIn={setStaff} />
          )}
        </div>
      </main>
    </div>
  );
}

type Failure = { title: string; detail?: string; correlationId?: string };

function Quoting({ staff, onSessionLost }: { staff: Staff; onSessionLost: () => void }) {
  const [input, setInput] = useState<QuoteInput>(EMPTY);
  const [errors, setErrors] = useState<FieldError[]>([]);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [forbidden, setForbidden] = useState<string | undefined>();
  const [quote, setQuote] = useState<Quote | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit() {
    // The vendor's quote generation is not idempotent, so a second submit while
    // one is in flight could bill twice. The button is disabled during a
    // request; this guards the paths that do not go through it.
    if (submitting) return;

    const found = validate(input);
    setErrors(found);
    setFailure(null);
    if (found.length > 0) return;

    setSubmitting(true);
    try {
      setQuote(
        await api.quote({
          loanAmount: Number(input.loanAmount),
          loanTermInMonths: Number(input.loanTermInMonths),
          riskBand: input.riskBand,
        }),
      );
    } catch (error) {
      handle(error as RequestFailed);
    } finally {
      setSubmitting(false);
    }
  }

  function handle(error: RequestFailed) {
    const { code, message, details, correlationId } = error.error;

    if (code === 'UNAUTHENTICATED') {
      onSessionLost();
      return;
    }
    if (code === 'FORBIDDEN') {
      setForbidden(correlationId);
      return;
    }
    // The Middleware validated something the front end let through, which means
    // the two have drifted. Render it as field errors rather than losing it.
    if (details?.length) {
      setErrors(details);
      return;
    }
    setFailure({
      title: message,
      detail: 'No quote was created, so nothing is duplicated by trying again.',
      correlationId,
    });
  }

  if (forbidden !== undefined) {
    return <NotEntitled correlationId={forbidden} />;
  }

  return (
    <>
      {quote ? (
        <QuoteSummary input={input} staffName={staff.name} onEdit={() => setQuote(null)} />
      ) : (
        <QuoteForm
          input={input}
          onChange={setInput}
          onSubmit={submit}
          errors={errors}
          failure={failure ?? undefined}
          submitting={submitting}
          staffName={staff.name}
        />
      )}
      {quote && <QuoteResult quote={quote} />}
    </>
  );
}

/** Nothing a retry would achieve, so no form and no button. */
function NotEntitled({ correlationId }: { correlationId?: string }) {
  return (
    <div className="card">
      <div style={{ display: 'flex', gap: 12, color: 'var(--cq-primary)' }}>
        <LockIcon />
        <div style={{ color: 'var(--cq-text)' }}>
          <h1 className="card__title" style={{ fontSize: 20 }}>
            You do not have access to generate quotes.
          </h1>
          <p className="card__lede" style={{ margin: 0 }}>
            You are signed in, so signing in again will not help. Ask your manager to have quote
            access added to your profile.
          </p>
          {correlationId && <Reference id={correlationId} />}
        </div>
      </div>
    </div>
  );
}

