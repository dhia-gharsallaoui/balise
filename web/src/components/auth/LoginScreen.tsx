import { useState, type FormEvent } from "react";
import { ApiError, login } from "../../lib/api";
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
      <form className="auth-card" onSubmit={handleSubmit}>
        <h1 className="auth-title">Balise</h1>
        <p className="auth-subtitle">Enter the owner password to continue.</p>
        <label className="auth-field" htmlFor="auth-password">
          <span>Password</span>
          <input
            id="auth-password"
            type="password"
            autoComplete="current-password"
            autoFocus
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            disabled={busy}
          />
        </label>
        {error ? (
          <p className="auth-error" role="alert">
            {error}
          </p>
        ) : null}
        <button type="submit" className="auth-submit" disabled={busy || password.length === 0}>
          {busy ? "Checking…" : "Unlock"}
        </button>
      </form>
    </div>
  );
}
