import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { parse } from 'yaml';
import { MAX_AMOUNT, MAX_MONTHS, MIN_AMOUNT, MIN_MONTHS, RISK_BANDS, validate } from './validation';

const input = (over: Partial<Parameters<typeof validate>[0]> = {}) => ({
  loanAmount: '250000.00',
  loanTermInMonths: '240',
  riskBand: 'B',
  ...over,
});

const codeFor = (field: string, over: Parameters<typeof input>[0]) =>
  validate(input(over)).find((e) => e.field === field)?.code;

describe('validation mirrors contract.md section 4', () => {
  it('accepts a valid request', () => {
    expect(validate(input())).toEqual([]);
  });

  it.each([
    ['', 'amount_invalid'],
    ['abc', 'amount_invalid'],
    ['1e6', 'amount_invalid'],
    ['-250000', 'amount_invalid'],
    ['999999.999', 'amount_precision'],
    ['999.99', 'amount_out_of_range'],
    ['5000000.01', 'amount_out_of_range'],
    ['0', 'amount_out_of_range'],
  ])('rejects loanAmount %s as %s', (loanAmount, code) => {
    expect(codeFor('loanAmount', { loanAmount })).toBe(code);
  });

  it.each([
    ['', 'term_invalid'],
    ['abc', 'term_invalid'],
    ['12.5', 'term_not_integer'],
    ['5', 'term_out_of_range'],
    ['361', 'term_out_of_range'],
    ['0', 'term_out_of_range'],
  ])('rejects loanTermInMonths %s as %s', (loanTermInMonths, code) => {
    expect(codeFor('loanTermInMonths', { loanTermInMonths })).toBe(code);
  });

  it.each(['', 'b', 'D', ' B'])('rejects riskBand %s', (riskBand) => {
    expect(codeFor('riskBand', { riskBand })).toBe('risk_band_invalid');
  });

  it('accepts the boundaries themselves', () => {
    expect(validate(input({ loanAmount: '1000.00', loanTermInMonths: '6', riskBand: 'A' }))).toEqual([]);
    expect(validate(input({ loanAmount: '5000000.00', loanTermInMonths: '360', riskBand: 'C' }))).toEqual([]);
  });

  it('reports every failure at once, not the first', () => {
    const errors = validate({ loanAmount: '1', loanTermInMonths: '9999', riskBand: 'Z' });
    expect(errors.map((e) => e.field).sort()).toEqual([
      'loanAmount',
      'loanTermInMonths',
      'riskBand',
    ]);
  });
});

// TypeScript cannot import a Go constant, so the same drift guard the services
// use applies here: the published contract is the shared source of truth.
describe('bounds match the published contract', () => {
  const spec = parse(readFileSync('../api/middleware.openapi.yaml', 'utf8'));
  const props = spec.components.schemas.QuoteRequest.properties;

  it('loanAmount range', () => {
    expect(props.loanAmount.minimum).toBe(MIN_AMOUNT);
    expect(props.loanAmount.maximum).toBe(MAX_AMOUNT);
  });

  it('loanTermInMonths range', () => {
    expect(props.loanTermInMonths.minimum).toBe(MIN_MONTHS);
    expect(props.loanTermInMonths.maximum).toBe(MAX_MONTHS);
  });

  it('risk bands', () => {
    expect([...props.riskBand.enum].sort()).toEqual([...RISK_BANDS].sort());
  });
});
