// Field codes are the contract; the wording is ours. An unknown code still says
// something, so a rule added to the Middleware cannot produce a form that
// refuses input without explaining why.

import { MAX_AMOUNT, MAX_MONTHS, MIN_AMOUNT, MIN_MONTHS } from './validation';

const money = new Intl.NumberFormat('en-AU', {
  style: 'currency',
  currency: 'AUD',
});

const FIELD_MESSAGES: Record<string, string> = {
  amount_invalid: 'Enter a loan amount.',
  amount_precision: 'Use at most 2 decimal places.',
  amount_out_of_range: `Enter an amount between ${money.format(MIN_AMOUNT)} and ${money.format(MAX_AMOUNT)}.`,
  term_invalid: 'Enter a loan term in months.',
  term_not_integer: 'Enter a whole number of months.',
  term_out_of_range: `Enter a term between ${MIN_MONTHS} and ${MAX_MONTHS} months.`,
  risk_band_invalid: 'Select a risk band.',
  malformed_body: 'Check the details and try again.',
};

export function fieldMessage(code: string): string {
  return FIELD_MESSAGES[code] ?? 'Check this field.';
}

export const FIELD_LABELS: Record<string, string> = {
  loanAmount: 'Loan amount',
  loanTermInMonths: 'Loan term in months',
  riskBand: 'Risk band',
  body: 'Request',
};
