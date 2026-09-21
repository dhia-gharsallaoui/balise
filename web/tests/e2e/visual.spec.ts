import { expect, test } from "@playwright/test";

// Task 3 (2026-09-16 frontend slice): these baselines were going stale because they
// screenshot against the same dev server (localhost:5173 / :8099) that knowledge.spec.ts
// and a11y.spec.ts use — and that server talks to whatever backend/vault happens to be
// running, which mutates on every pipeline run. Pointing this file's baseURL at the
// frozen fixture vault (a small committed vault reindexed into its own Postgres schema
// and served on a distinct port — see tests/e2e/fixtures/run-fixture-backend.mjs and
// playwright.config.ts's webServer array) makes every test below deterministic without
// touching knowledge.spec.ts/a11y.spec.ts, which intentionally keep exercising whatever
// vault is actually running.
test.use({ baseURL: "http://localhost:5271" });

// Batch G Task 8, spec §12 criterion 9: four breakpoints, both themes.

const BREAKPOINTS = [
  { name: "320", width: 320, height: 800 },
  { name: "768", width: 768, height: 900 },
  { name: "1024", width: 1024, height: 900 },
  { name: "1440", width: 1440, height: 900 },
];

for (const theme of ["light", "dark"] as const) {
  for (const size of BREAKPOINTS) {
    test(`knowledge at ${size.name} in ${theme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.goto("/");
      await page.waitForSelector(".page-row");
      await expect(page).toHaveScreenshot(`knowledge-${size.name}-${theme}.png`, {
        fullPage: true,
        maxDiffPixelRatio: 0.01,
      });
    });

    test(`no horizontal overflow at ${size.name} in ${theme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme });
      await page.setViewportSize({ width: size.width, height: size.height });
      await page.goto("/");
      const overflows = await page.evaluate(
        () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
      );
      expect(overflows).toBe(false);
    });
  }
}

test("two browse columns at 1440 and 1024, one at 768; opening a page replaces them all", async ({ page }) => {
  // Owner's own call, now settled (see reading-view-report.md): there is no longer a third
  // (400px reader) column at any width. Opening a page takes over the whole screen as a
  // dedicated full-width reading page instead of sharing it with the browse grid, so the
  // column-count checks below run with nothing open, and a separate check at the end confirms
  // the grid is replaced entirely (not just given a third column) once a page is opened.
  await page.goto("/");
  const columns = async () =>
    (await page.locator(".kn-grid").evaluate((el) => getComputedStyle(el).gridTemplateColumns))
      .split(" ").length;

  // Knowledge.tsx derives its column breakpoint from a native `resize` event
  // listener (see the classify()/useBreakpoint() comment there), which fires
  // and re-renders asynchronously relative to Playwright's setViewportSize().
  // Reading columns() immediately is a race — it intermittently observes the
  // pre-resize render. expect.poll retries until the app's own state catches
  // up instead of asserting against a single, possibly-stale snapshot.
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect.poll(columns).toBe(2);

  await page.setViewportSize({ width: 1024, height: 900 });
  await expect.poll(columns).toBe(2);

  await page.setViewportSize({ width: 768, height: 900 });
  await expect.poll(columns).toBe(1);

  // Opening a page replaces the whole browse grid (space tree + toolbar + list/graph) with
  // the dedicated reading page — checked here at 768, but the rule has no width exception.
  await page.locator(".page-open").first().click();
  await expect(page.locator(".kn-reader")).toBeVisible();
  await expect(page.locator(".kn-grid")).toHaveCount(0);
});
