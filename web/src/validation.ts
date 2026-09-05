// Mirrors design/contract.md section 4 for immediate feedback. The Middleware
// validates independently and is the authority; this only saves a round trip.
// A test asserts these bounds match the published OpenAPI contract.

export const MIN_AMOUNT = 1000;
export const MAX_AMOUNT = 5000000;
export const MIN_MONTHS = 6;
export const MAX_MONTHS = 360;
export const RISK_BANDS = ['A', 'B', 'C'] as const;

export type RiskBand = (typeof RISK_BANDS)[number];

export type FieldError = { field: string; code: string };

export type QuoteInput = {
  loanAmount: string;
  loanTermInMonths: string;
  riskBand: string;
};

const PLAIN_DECIMAL = /^\d+(\.\d{1,2})?$/;
const TOO_PRECISE = /^\d+\.\d{3,}$/;
const WHOLE_NUMBER = /^\d+$/;

export function validate(input: QuoteInput): FieldError[] {
  const errors: FieldError[] = [];
  const amount = input.loanAmount.trim();
  const term = input.loanTermInMonths.trim();

  if (amount === '') {
    errors.push({ field: 'loanAmount', code: 'amount_invalid' });
  } else if (TOO_PRECISE.test(amount)) {
    errors.push({ field: 'loanAmount', code: 'amount_precision' });
  } else if (!PLAIN_DECIMAL.test(amount)) {
    errors.push({ field: 'loanAmount', code: 'amount_invalid' });
  } else if (Number(amount) < MIN_AMOUNT || Number(amount) > MAX_AMOUNT) {
    errors.push({ field: 'loanAmount', code: 'amount_out_of_range' });
  }

  if (term === '') {
    errors.push({ field: 'loanTermInMonths', code: 'term_invalid' });
  } else if (term.includes('.')) {
    errors.push({ field: 'loanTermInMonths', code: 'term_not_integer' });
  } else if (!WHOLE_NUMBER.test(term)) {
    errors.push({ field: 'loanTermInMonths', code: 'term_invalid' });
  } else if (Number(term) < MIN_MONTHS || Number(term) > MAX_MONTHS) {
    errors.push({ field: 'loanTermInMonths', code: 'term_out_of_range' });
  }

  if (!RISK_BANDS.includes(input.riskBand as RiskBand)) {
    errors.push({ field: 'riskBand', code: 'risk_band_invalid' });
  }

  return errors;
}
