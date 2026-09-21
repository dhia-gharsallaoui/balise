import { defineConfig, devices } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { STORAGE_STATE_PATH } from "./tests/e2e/global-setup";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Batch G Task 8. Deviation from task-8-brief.md's sample config: only the
// chromium project is configured. "Install nothing" is a hard constraint for
// this batch — Chrome/Playwright are already present on this machine, but
// only the chromium browser is cached under ~/.cache/ms-playwright (no
// firefox-*/webkit-* directories); adding those projects would force
// `playwright test` to try to launch browsers that were never installed
// here, which this batch is explicitly forbidden from installing. See
// batch-G-report.md for the full note.

// Single source of truth for the password every backend in this suite runs with, and the
// one global-setup.ts logs in with. Every /api route now sits behind internal/api/auth.go's
// requireSession, so a known password has to reach three places that cannot share a normal
// module import (a .mjs script, a .ts test file, and whatever process is already serving
// :8099): this file's own webServer `env` below (the :8199 fixture backend), the externally
// -started :8099 backend (see the first webServer entry's comment), and global-setup.ts's
// login call. Writing the resolved value back onto process.env here — once — lets all three
// agree on it without an import across that boundary.
const TEST_PASSWORD = process.env.BALISE_TEST_PASSWORD ?? "playwright-test-password";
process.env.BALISE_TEST_PASSWORD = TEST_PASSWORD;

export default defineConfig({
  testDir: "./tests/e2e",
  // Logs in once via the real /api/auth/login endpoint and persists the resulting session
  // cookie to STORAGE_STATE_PATH; every project below starts each test already authenticated
  // instead of re-running a login flow 47 times or weakening the tests with an auth bypass.
  globalSetup: "./tests/e2e/global-setup.ts",
  // These are integration tests against a live Go backend and a real Postgres, not unit
  // tests against mocks, and four workers share one dev backend that is simultaneously
  // serving a hundred-odd real pages while the fixture backend beside it reindexes. Under
  // that contention a first paint can genuinely take longer than Playwright's 5s default,
  // which showed up as roughly one failure in three, moving between assertions — a list
  // that had not rendered, a toolbar that had not mounted. Each looked like a different
  // bug; all of them were the same clock running out. Raising the bound waits for a slow
  // round trip rather than declaring the app broken. A real regression still fails, just
  // after 15s instead of 5s.
  expect: { timeout: 15_000 },
  // Two workers, not the default four. Every spec loads the app from the Vite *dev* server,
  // which serves hundreds of unbundled ES modules per page load from a single Node process;
  // four workers cold-loading at once starve it while the Go backend answers in ~170ms. The
  // symptom was a first paint that never arrived, surfacing at whichever assertion happened
  // to be first — a missing list, a missing toolbar — at about one run in three.
  workers: 2,
  // One retry, because these drive a real browser against a real server and a genuinely
  // stuck first paint is not a product defect. Playwright reports anything that passes on
  // retry as "flaky" rather than "passed", so this hides nothing: a test that needs the
  // retry still shows up and can be chased. A test that fails twice is a real failure.
  retries: 1,
  use: {
    baseURL: "http://localhost:5173",
    trace: "on-first-retry",
    storageState: STORAGE_STATE_PATH,
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // Three servers, not one: knowledge.spec.ts/a11y.spec.ts keep exercising whatever
  // dev server + backend is already running (localhost:5173 / :8099, first entry below,
  // unchanged from before). visual.spec.ts overrides its own baseURL to localhost:5271
  // (see that file) so its screenshots run against a frozen fixture vault instead — the
  // second and third entries bring that vault's backend (a Node wrapper script that
  // assembles, commits, reindexes and serves it on :8199 — see
  // tests/e2e/fixtures/run-fixture-backend.mjs) and its own Vite dev server up.
  webServer: [
    {
      command: "npm run dev",
      url: "http://localhost:5173",
      reuseExistingServer: true,
      // This entry only starts the Vite frontend, never a Go backend — reuseExistingServer
      // assumes something is already listening on :8099 (typically `make up`). Now that every
      // /api route requires a session, that backend must be started with
      // BALISE_PASSWORD=<the value TEST_PASSWORD resolves to above>, e.g.
      // `make up PASSWORD=playwright-test-password`, or global-setup.ts's login fails closed
      // with a clear error naming this requirement rather than every spec silently 401ing.
    },
    {
      command: "node tests/e2e/fixtures/run-fixture-backend.mjs",
      url: "http://127.0.0.1:8199/api/tree",
      reuseExistingServer: false,
      timeout: 60_000,
      // Merged onto this command's process.env; run-fixture-backend.mjs's own child_process
      // .spawn call for `balise serve` sets no explicit `env`, so Node inherits this value
      // straight through to the Go backend without any further wiring.
      env: { BALISE_PASSWORD: TEST_PASSWORD },
    },
    {
      command: "npx vite --port 5271 --strictPort",
      url: "http://localhost:5271",
      reuseExistingServer: false,
      env: { VITE_API_BASE: "http://localhost:8199" },
    },
  ],
});
