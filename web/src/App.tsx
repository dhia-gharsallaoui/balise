import { useEffect, useState } from "react";
import { AppShell } from "./components/AppShell";
import { LoginScreen } from "./components/auth/LoginScreen";
import { fetchAuthStatus } from "./lib/api";

type AuthState = "checking" | "required" | "authenticated";

// Gates AppShell strictly: AppShell's own effects (fetchTree, fetchHome) must never fire
// while unauthenticated, so the check happens here, one level up, not inside AppShell
// itself. "checking" renders nothing rather than a flash of either screen while the one
// GET /api/auth/status round trip resolves.
export default function App() {
  const [authState, setAuthState] = useState<AuthState>("checking");

  useEffect(() => {
    let cancelled = false;
    fetchAuthStatus()
      .then((status) => {
        if (cancelled) return;
        setAuthState(!status.auth_required || status.authenticated ? "authenticated" : "required");
      })
      .catch(() => {
        // fetchAuthStatus never throws for a real 401 (it reads the JSON body directly rather
        // than treating a non-2xx as an error — see lib/api.ts), so this only catches a genuine
        // network failure reaching the endpoint. Erring toward "required" here: a login screen
        // that failed to load is a nuisance, a vault that failed open is the bug this feature
        // exists to close.
        if (!cancelled) setAuthState("required");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (authState === "checking") return null;
  if (authState === "required") {
    return <LoginScreen onSuccess={() => setAuthState("authenticated")} />;
  }
  return <AppShell />;
}
