import { formatMoney, formatRate } from '../format';
import type { Quote } from '../api';
import { CheckIcon } from './icons';

/** Presents the vendor's figures. Nothing here is recalculated. */
export function QuoteResult({ quote }: { quote: Quote }) {
  return (
    <section className="card" aria-labelledby="result-heading">
      <h2 className="result__heading" id="result-heading" tabIndex={-1}>
        <CheckIcon />
        Quote generated
      </h2>

      <div className="result__figures">
        <div>
          <div className="result__label">Commission rate</div>
          <div className="result__figure">{formatRate(quote.commissionRate)}</div>
        </div>
        <div>
          <div className="result__label">Total commission</div>
          <div className="result__figure">{formatMoney(quote.totalCommission)}</div>
        </div>
      </div>

      <div className="result__id">
        <div className="result__label">Quote ID</div>
        <div className="mono">{quote.quoteId}</div>
      </div>

      <p className="result__note">
        Advisory only and not binding. Nothing is stored, so generate a new quote rather than
        returning to this one.
      </p>
    </section>
  );
}
