export const Mark = () => (
  <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor"
       strokeWidth="1.75" strokeLinecap="round" aria-hidden="true">
    <circle cx="7.5" cy="7.5" r="3.25" />
    <circle cx="16.5" cy="16.5" r="3.25" />
    <line x1="18.5" y1="5.5" x2="5.5" y2="18.5" />
  </svg>
);

export const AlertIcon = ({ size = 16 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor"
       strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"
       style={{ flex: 'none', marginTop: 1 }} aria-hidden="true">
    <path d="M8 2.5 14.5 13.5H1.5Z" />
    <line x1="8" y1="6.4" x2="8" y2="9.4" />
    <circle cx="8" cy="11.6" r="0.6" fill="currentColor" stroke="none" />
  </svg>
);

export const ClockIcon = () => (
  <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor"
       strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"
       style={{ flex: 'none', marginTop: 1 }} aria-hidden="true">
    <circle cx="10" cy="10" r="7.5" />
    <path d="M10 6v4.5l3 1.8" />
  </svg>
);

export const LockIcon = () => (
  <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor"
       strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round"
       style={{ flex: 'none', marginTop: 2 }} aria-hidden="true">
    <rect x="4" y="9" width="12" height="8" rx="1.5" />
    <path d="M7 9V6.5a3 3 0 0 1 6 0V9" />
  </svg>
);

export const CheckIcon = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="currentColor"
       strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M3.5 9.5 7 13l7.5-8" />
  </svg>
);

export const Chevron = () => (
  <svg className="field__chevron" width="14" height="14" viewBox="0 0 16 16" fill="none"
       stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round"
       aria-hidden="true">
    <path d="M4 6.5 8 10.5l4-4" />
  </svg>
);

export const Spinner = () => (
  <svg className="btn__spinner" width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <circle cx="9" cy="9" r="7" stroke="rgb(255 255 255 / 35%)" strokeWidth="2" />
    <path d="M9 2a7 7 0 0 1 7 7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
  </svg>
);
