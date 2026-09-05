import { useState, type FormEvent } from 'react';
import { api, RequestFailed, type Staff } from '../api';
import { Banner } from './Banner';

export function SignIn({ onSignedIn }: { onSignedIn: (staff: Staff) => void }) {
  const [staffId, setStaffId] = useState('');
  const [password, setPassword] = useState('');
  const [failure, setFailure] = useState<RequestFailed | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setFailure(null);
    try {
      onSignedIn(await api.signIn(staffId, password));
    } catch (error) {
      setFailure(error as RequestFailed);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card" onSubmit={submit} noValidate>
      <h1 className="card__title">Sign in</h1>
      <p className="card__lede">Commission quotes for lending staff.</p>

      {failure && (
        <div style={{ marginBottom: 20 }}>
          <Banner tone="danger" title={failure.error.message} correlationId={failure.error.correlationId}>
            <div className="banner__detail">Check both and try again.</div>
          </Banner>
        </div>
      )}

      <div className="stack">
        <div>
          <label className="field__label" htmlFor="staffId">
            Staff ID
          </label>
          <input
            className="field__input"
            id="staffId"
            name="staffId"
            autoComplete="username"
            value={staffId}
            onChange={(e) => setStaffId(e.target.value)}
          />
        </div>
        <div>
          <label className="field__label" htmlFor="password">
            Password
          </label>
          <input
            className="field__input"
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <button className="btn btn--block" type="submit" disabled={busy} aria-busy={busy}>
          {busy ? 'Signing in' : 'Sign in'}
        </button>
      </div>
    </form>
  );
}
