import { request } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Every spec in this suite now sits behind internal/api/auth.go's requireSession, so a
// fresh, cookie-less browser context 401s on its very first request — all 47 existing specs
// (a11y, visual, knowledge, urlstate) would fail closed without this. Logging in once here
// and handing every project `storageState: STORAGE_STATE_PATH` (see playwright.config.ts)
// avoids repeating a login flow per spec, and matches Playwright's own recommended
// "authenticate once in global setup, reuse storageState" recipe rather than adding any
// test-only auth bypass (which the task this file exists for explicitly forbids).
export const STORAGE_STATE_PATH = path.join(__dirname, ".auth", "owner.json");

// One login covers every webServer pair below. The session cookie internal/api/auth.go
// issues carries no Domain attribute, so it is host-only-scoped to "localhost" rather than
// to a single port: the browser presents it to :5173, to :8099 (proxied through :5173), and
// to :5271/:8199 alike — *provided* the :8199 fixture backend was started with the same
// password, so its independently-derived HKDF signing key matches (see
// internal/api/auth_test.go's TestSessionSignedUnderDifferentPasswordIsRejected for the
// property this relies on, and its mirror image here).
export default async function globalSetup(): Promise<void> {
  const password = process.env.BALISE_TEST_PASSWORD;
  if (!password) {
    throw new Error(
      "BALISE_TEST_PASSWORD is unset. playwright.config.ts should have defaulted it before " +
        "globalSetup ran — every /api route 401s without a session, so the suite cannot " +
        "proceed without a known password to log in with.",
    );
  }

  const context = await request.newContext({ baseURL: "http://localhost:5173" });
  try {
    const response = await context.post("/api/auth/login", { data: { password } });
    if (!response.ok()) {
      throw new Error(
        `POST http://localhost:5173/api/auth/login failed with ${response.status()}. Is the ` +
          "dev backend on :8099 running with BALISE_PASSWORD set to the same value as " +
          "BALISE_TEST_PASSWORD? a11y.spec.ts/knowledge.spec.ts/urlstate.spec.ts assume that " +
          "backend is already up (e.g. via `make up PASSWORD=...`) — this webServer entry only " +
          "starts the Vite frontend, not the Go backend (see playwright.config.ts).",
      );
    }
    await context.storageState({ path: STORAGE_STATE_PATH });
  } finally {
    await context.dispose();
  }
}
