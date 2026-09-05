import type { ReactNode } from 'react';
import { AlertIcon } from './icons';

type Props = {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children: (props: {
    id: string;
    'aria-invalid': boolean;
    'aria-describedby': string | undefined;
  }) => ReactNode;
};

/** Labels the control, marks it invalid, and points it at the message. */
export function Field({ id, label, hint, error, children }: Props) {
  const messageId = error ? `${id}-error` : hint ? `${id}-hint` : undefined;

  return (
    <div>
      <label className="field__label" htmlFor={id}>
        {label}
      </label>
      {children({ id, 'aria-invalid': Boolean(error), 'aria-describedby': messageId })}
      {error ? (
        <div className="field__error" id={messageId}>
          <AlertIcon />
          <span>{error}</span>
        </div>
      ) : (
        hint && (
          <div className="field__hint" id={messageId}>
            {hint}
          </div>
        )
      )}
    </div>
  );
}
