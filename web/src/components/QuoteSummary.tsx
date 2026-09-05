import { formatMoney } from '../format';
import type { QuoteInput } from '../validation';

type Props = {
  input: QuoteInput;
  staffName: string;
  onEdit: () => void;
};

/**
 * The form, collapsed in place after submitting. It keeps its position so the
 * page does not rearrange itself, and it carries the values the quote was
 * produced from: a quote cannot be checked without them.
 */
export function QuoteSummary({ input, staffName, onEdit }: Props) {
  return (
    <div className="card card--tint">
      <div className="summary__signed-in">
        Signed in as <strong>{staffName}</strong>
      </div>
      <div className="summary">
        <div>
          <div className="result__label">Quote for</div>
          <div className="summary__value">
            {formatMoney(Number(input.loanAmount))} &middot; {input.loanTermInMonths} months &middot;
            Band {input.riskBand}
          </div>
        </div>
        <button className="btn btn--quiet" type="button" onClick={onEdit}>
          Edit
        </button>
      </div>
    </div>
  );
}
