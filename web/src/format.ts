// Presentation only. The figures are the vendor's and are never recalculated.

const money = new Intl.NumberFormat('en-AU', {
  style: 'currency',
  currency: 'AUD',
});

const percent = new Intl.NumberFormat('en-AU', {
  style: 'percent',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

export const formatMoney = (value: number): string => money.format(value);
export const formatRate = (value: number): string => percent.format(value);
