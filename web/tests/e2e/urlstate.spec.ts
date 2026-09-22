import { expect, test } from "@playwright/test";

// The address bar is the app's only sharing mechanism: there is no "copy link" button, so
// whatever a person is looking at has to be reconstructible from the URL they copy. These
// run against the live backend at :8099 (see playwright.config.ts), so they assert on
// behaviour and URL shape rather than on any particular page in the corpus.

type Page = import("@playwright/test").Page;

const searchBox = (page: Page) => page.getByPlaceholder("Search claims…");
const viewButton = (page: Page, mode: "List" | "Graph") =>
  page.locator(".kn-viewmode button", { hasText: mode });

const path = (page: Page) => {
  const u = new URL(page.url());
  return u.pathname + u.search;
};

/**
 * Waits for the list to have loaded before interacting, so a fill never races hydration.
 *
 * Worth knowing if this ever fails: the captured snapshot reads "0 scopes · 0 pages", which
 * looks like the backend returned nothing but is just `tree === null` before the fetch
 * resolves — the loading state, not an error state. The suite-wide expect timeout in
 * playwright.config.ts is what gives it room under parallel load.
 */
async function ready(page: Page) {
  await expect(page.locator(".page-row").first()).toBeVisible();
}

test("a shared search link reconstructs the search", async ({ page }) => {
  await page.goto("/knowledge");
  await ready(page);
  const unfiltered = await page.locator(".page-row").count();

  await page.goto("/knowledge?q=alloy");
  await expect(searchBox(page)).toHaveValue("alloy");
  await ready(page);
  // Narrowed, not just re-rendered: the shared link shows the searched set.
  expect(await page.locator(".page-row").count()).toBeLessThan(unfiltered);
});

test("typing writes the query without pushing a history entry per keystroke", async ({ page }) => {
  await page.goto("/knowledge");
  await ready(page);
  const before = await page.evaluate(() => history.length);

  await searchBox(page).fill("promql");
  await expect.poll(() => path(page)).toBe("/knowledge?q=promql");
  // Replace, not push: one entry per character would bury the previous screen and make the
  // back button useless.
  expect(await page.evaluate(() => history.length)).toBe(before);
});

test("changing section pushes a path, and back restores the previous view", async ({ page }) => {
  await page.goto("/knowledge");
  await ready(page);
  await searchBox(page).fill("promql");
  await expect.poll(() => path(page)).toBe("/knowledge?q=promql");

  await page.getByRole("button", { name: "Review", exact: true }).click();
  await expect.poll(() => path(page)).toBe("/review");

  await page.goBack();
  await expect.poll(() => path(page)).toBe("/knowledge?q=promql");
  await expect(searchBox(page)).toHaveValue("promql");
});

test("an opened page is in the URL, and that URL reopens it", async ({ page }) => {
  await page.goto("/knowledge");
  await ready(page);
  await page.locator(".page-open").first().click();
  // Opening a page from List now takes over the whole screen as a dedicated reading page
  // (`.kn-reader`, see reading-view-report.md) instead of sharing it with a `.kn-right` column.
  await expect(page.locator(".kn-reader")).toBeVisible();

  const shared = path(page);
  expect(shared).toContain("page=");
  // Readable rather than percent-encoded — these links get pasted into messages.
  expect(shared).not.toContain("%2F");
  const title = await page.locator(".kn-reader h2, .kn-reader h3").first().textContent();

  await page.goto(shared);
  await expect(page.locator(".kn-reader")).toBeVisible();
  expect(await page.locator(".kn-reader h2, .kn-reader h3").first().textContent()).toBe(title);
});

test("view and open page compose into one link", async ({ page }) => {
  // The graph is an ego graph — it centres on the open page, so `view=Graph` is only
  // meaningful alongside `page=`. That the two compose is the point of putting both in
  // the URL rather than only the search query.
  //
  // This opens the page from within Graph view (clicking a global node) rather than opening
  // it from List first: opening from List now shows the dedicated `.kn-reader` full-width
  // page (reading-view-report.md), which takes over the whole screen — including the
  // toolbar the Graph toggle lives in — so there would be no Graph button left to click.
  await page.goto("/knowledge");
  await ready(page);
  await viewButton(page, "Graph").click();

  // Wait for the graph itself before reaching for a node: the toggle only starts the
  // /api/graph fetch and the layout pass, so nodes exist a beat later than the click.
  await expect(page.locator(".graph-frame")).toBeVisible();
  // A node with relations, not merely the first in the DOM — an isolated node sits on the
  // shelf and is a different shape of target. The canvas draws to <canvas>, so this drives
  // GraphA11yList's real, focusable button equivalent instead of a pointer click.
  const node = page.locator('.graph-a11y-node[data-isolated="false"]').first();
  await node.focus();
  await page.keyboard.press("Enter");

  // Poll rather than read straight after the click. Opening a page is a React state update
  // followed by a history write, so reading the URL synchronously races both — which is
  // exactly what made this test fail about one full-suite run in two while passing alone
  // and passing three times over as a single file.
  await expect.poll(() => path(page)).toContain("page=");
  const shared = path(page);
  expect(shared).toContain("view=Graph");

  await page.goto(shared);
  await expect(viewButton(page, "Graph")).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".graph-frame")).toBeVisible();
});

test("a malformed link still opens the app", async ({ page }) => {
  await page.goto("/nonsense?page=broken&view=sideways&hide=");
  // Unknown section, unparseable page and unknown view all fall back rather than erroring.
  await expect(searchBox(page)).toBeVisible();
  await ready(page);
  await expect(viewButton(page, "List")).toHaveAttribute("aria-pressed", "true");
});
