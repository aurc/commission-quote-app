import { useRef, type FormEvent } from 'react';
import { FIELD_LABELS, fieldMessage } from '../messages';
import { MAX_AMOUNT, MAX_MONTHS, MIN_AMOUNT, MIN_MONTHS, RISK_BANDS } from '../validation';
import type { FieldError, QuoteInput } from '../validation';
import { formatMoney } from '../format';
import { Banner } from './Banner';
import { Field } from './Field';
import { Chevron, Spinner } from './icons';

type Props = {
  input: QuoteInput;
  onChange: (input: QuoteInput) => void;
  onSubmit: () => void;
  errors: FieldError[];
  failure?: { title: string; detail?: string; correlationId?: string };
  submitting: boolean;
  staffName: string;
};

const BAND_LABELS: Record<string, string> = {
  A: 'A — Low risk',
  B: 'B — Standard risk',
  C: 'C — Elevated risk',
};

export function QuoteForm({
  input,
  onChange,
  onSubmit,
  errors,
  failure,
  submitting,
  staffName,
}: Props) {
  const summaryRef = useRef<HTMLDivElement>(null);
  const errorFor = (field: string) => errors.find((e) => e.field === field)?.code;

  function submit(event: FormEvent) {
    event.preventDefault();
    onSubmit();
    // Sighted and screen reader users both land on the problem rather than
    // hunting for it.
    queueMicrotask(() => summaryRef.current?.focus());
  }

  const set = (patch: Partial<QuoteInput>) => onChange({ ...input, ...patch });

  return (
    <form className="card" onSubmit={submit} noValidate>
      <h1 className="card__title">Generate a commission quote</h1>
      <p className="card__lede">Enter the loan details. The quote is advisory and is not binding.</p>

      <div className="stack">
        <div className="summary__signed-in">
          Signed in as <strong>{staffName}</strong>
        </div>

        {failure && (
          <Banner tone="notice" title={failure.title} correlationId={failure.correlationId}>
            {failure.detail && <div className="banner__detail">{failure.detail}</div>}
          </Banner>
        )}

        {errors.length > 0 && (
          <div className="banner banner--danger" role="alert" tabIndex={-1} ref={summaryRef}>
            <div>
              <div className="banner__title">Check the highlighted fields.</div>
              <ul className="banner__list">
                {errors.map((error) => (
                  <li key={error.field}>
                    <a href={`#${error.field}`}>
                      {FIELD_LABELS[error.field]}: {fieldMessage(error.code)}
                    </a>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        )}

        <Field
          id="loanAmount"
          label="Loan amount"
          hint={`Between ${formatMoney(MIN_AMOUNT)} and ${formatMoney(MAX_AMOUNT)}, in dollars and cents.`}
          error={errorFor('loanAmount') && fieldMessage(errorFor('loanAmount')!)}
        >
          {(field) => (
            <div className="field__control">
              <span className="field__prefix" aria-hidden="true">
                $
              </span>
              <input
                {...field}
                className="field__input field__input--prefixed"
                inputMode="decimal"
                value={input.loanAmount}
                onChange={(e) => set({ loanAmount: e.target.value })}
              />
            </div>
          )}
        </Field>

        <Field
          id="loanTermInMonths"
          label="Loan term in months"
          hint={`Between ${MIN_MONTHS} and ${MAX_MONTHS} months.`}
          error={errorFor('loanTermInMonths') && fieldMessage(errorFor('loanTermInMonths')!)}
        >
          {(field) => (
            <input
              {...field}
              className="field__input"
              inputMode="numeric"
              value={input.loanTermInMonths}
              onChange={(e) => set({ loanTermInMonths: e.target.value })}
            />
          )}
        </Field>

        <Field
          id="riskBand"
          label="Risk band"
          hint="A low, B standard, C elevated."
          error={errorFor('riskBand') && fieldMessage(errorFor('riskBand')!)}
        >
          {(field) => (
            <div className="field__control">
              <select
                {...field}
                className="field__select"
                value={input.riskBand}
                onChange={(e) => set({ riskBand: e.target.value })}
              >
                <option value="">Choose a band</option>
                {RISK_BANDS.map((band) => (
                  <option key={band} value={band}>
                    {BAND_LABELS[band]}
                  </option>
                ))}
              </select>
              <Chevron />
            </div>
          )}
        </Field>

        <button className="btn btn--block" type="submit" disabled={submitting} aria-busy={submitting}>
          {submitting ? (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10 }}>
              <Spinner />
              Generating quote
            </span>
          ) : (
            'Generate Quote'
          )}
        </button>

        {submitting && (
          <div className="field__hint" style={{ textAlign: 'center' }}>
            Contacting the quote service. This usually takes a moment.
          </div>
        )}
      </div>
    </form>
  );
}
