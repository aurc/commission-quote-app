import type { ReactNode } from 'react';
import { AlertIcon, ClockIcon } from './icons';

type Props = {
  tone: 'danger' | 'notice';
  title: string;
  children?: ReactNode;
  correlationId?: string;
};

/**
 * A refused sign in, failed validation and an unreachable vendor all take this
 * shape. Danger when the user can fix it, notice when they cannot.
 */
export function Banner({ tone, title, children, correlationId }: Props) {
  return (
    <div className={`banner banner--${tone}`} role="alert">
      {tone === 'danger' ? <AlertIcon size={18} /> : <ClockIcon />}
      <div>
        <div className="banner__title">{title}</div>
        {children}
        {correlationId && <Reference id={correlationId} />}
      </div>
    </div>
  );
}

export function Reference({ id }: { id: string }) {
  return (
    <div className="reference">
      <span>Reference</span>
      <span className="mono">{id}</span>
      <span>quote this if you contact support</span>
    </div>
  );
}
