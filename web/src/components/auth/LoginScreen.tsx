import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, login } from "../../lib/api";
import { WarningCircle } from "@phosphor-icons/react";
// shell.css supplies .rail-mark, the CSS-drawn Balise mark, so the login screen shows the same
// mark the rail does without AppShell (which normally imports it) being mounted.
import "../../styles/shell.css";
import "../../styles/auth.css";

interface LoginScreenProps {
  onSuccess: () => void;
}

// The single screen a viewer with no session ever sees: one password field, matching the
// owner's explicit ask for something simple ("I don't want to have complicated mechanism")
// and balise-docs/04-technical-spec-v1.md §13's "session cookie for the owner (local
// password or Tailscale identity header)" — no accounts, no registration, no password
// reset. A correct password sets the HttpOnly session cookie server-side (POST
// /api/auth/login); onSuccess just tells AppRoot to stop rendering this screen and re-check
// /api/auth/status.
export function LoginScreen({ onSuccess }: LoginScreenProps) {
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const passwordRef = useRef<HTMLInputElement>(null);

  // The input is disabled while a login is in flight, which drops focus. Once a failed
  // attempt re-enables it, put the cursor back so the owner can simply retype.
  useEffect(() => {
    if (error && !busy) passwordRef.current?.focus();
  }, [error, busy]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await login(password);
      onSuccess();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError("Incorrect password.");
      } else if (err instanceof ApiError && err.status === 429) {
        setError("Too many attempts. Wait a minute and try again.");
      } else {
        setError("Could not reach the server.");
      }
      setPassword("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth-page">
      <form className="panel auth-card" onSubmit={handleSubmit}>
        <div className="auth-brand">
          <span className="rail-mark" aria-hidden="true" />
          <h1 className="auth-title">Balise</h1>
        </div>
        <p className="auth-subtitle">Enter the owner password to continue.</p>
        <label className="auth-field" htmlFor="auth-password">
          <span className="auth-label">Password</span>
          <input
            id="auth-password"
            ref={passwordRef}
            className="field"
            type="password"
            autoComplete="current-password"
            autoFocus
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            disabled={busy}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "auth-error" : undefined}
          />
        </label>
        {error ? (
          <p id="auth-error" className="auth-error" role="alert">
            <WarningCircle size={16} aria-hidden="true" className="auth-error-icon" />
            {error}
          </p>
        ) : null}
        <button type="submit" className="btn btn-primary auth-submit" disabled={busy || password.length === 0}>
          {busy ? "Checking…" : "Unlock"}
        </button>
      </form>
    </div>
  );
}
