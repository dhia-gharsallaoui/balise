import { expect, test } from "@playwright/test";

// Batch G Task 8, spec §12 criteria 5, 6 and 8, against the live backend at
// :8099 serving /tmp/balise-live (see playwright.config.ts / batch-G-report.md
// for how this suite is run). One deviation from task-8-brief.md's sample:
// the search box's accessible name is "Search claims" in this codebase (see
// Knowledge.tsx), not "Search claims and pages" — the brief's sample text
// didn't match the actual aria-label, so it is corrected here.

test.beforeEach(async ({ page }) => await page.goto("/"));

test("criterion 5: claim rows grouped by page and type", async ({ page }) => {
  await expect(page.getByRole("heading", { level: 3 }).first()).toBeVisible();
  await expect(page.locator(".page-row").first()).toBeVisible();
});

// A page whose title comes from the hook rung (see resolveTitle in the importer) has its sole
// claim suppressed, because it would otherwise duplicate the title exactly (PageDetail.tsx /
// PageRow.tsx). Most pages in the real corpus are hook-titled, so the very first row is not
// reliably one with a claim left to show; pick the first row whose twist is still enabled
// instead of assuming row order.
const firstRowWithClaims = (page: import("@playwright/test").Page) =>
  page.locator(".page-row").filter({ has: page.locator(".page-twist:not([disabled])") }).first();

test("criterion 6: opening a page shows claims above the body", async ({ page }) => {
  await firstRowWithClaims(page).locator(".page-open").click();
  // Opening a page from List now takes over the whole screen as a dedicated reading page
  // (`.kn-reader`, see reading-view-report.md) instead of sharing it with a `.kn-right` column.
  const panel = page.locator(".kn-reader");
  await expect(panel.locator(".claim-ledger")).toBeVisible();
  const ledgerBox = await panel.locator(".claim-ledger").boundingBox();
  const bodyBox = await panel.locator(".detail-body").boundingBox();
  expect(ledgerBox!.y).toBeLessThan(bodyBox!.y);
});

test("claims expand in place under a page row", async ({ page }) => {
  const row = firstRowWithClaims(page);
  await row.getByRole("button", { name: /expand/i }).click();
  await expect(row.locator(".claim-row").first()).toBeVisible();
});

test("criterion 7: search shows a low-coverage state", async ({ page }) => {
  await page.getByLabel("Search claims").fill("quarterly revenue forecast");
  await expect(page.getByText(/Nothing matches/)).toBeVisible();
});

test("criterion 8: graph renders and navigates", async ({ page }) => {
  // The ego graph (spec §6.7 F-61) only ever appears once a page is centred; with nothing
  // open, Graph view shows the whole-vault global graph instead (see the test below). Opening
  // a page from List first (as this test used to) no longer works here: the dedicated reading
  // page (reading-view-report.md) takes over the whole screen, including the toolbar the Graph
  // toggle lives in, so this reaches the ego graph by switching to Graph view and clicking a
  // global node to centre it instead.
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  await expect(page.locator(".global-graph")).toBeVisible();
  // A node with at least one edge (`data-isolated="false"`) is guaranteed a >1-node ego graph.
  await page.locator('.global-node[data-isolated="false"]').first().click();
  await expect(page.getByRole("img", { name: /relation graph/i })).toBeVisible();

  // Clicking another node in the ego graph navigates further, re-centring on it.
  const otherNode = page.locator('.graph-node:not([data-depth="0"])').first();
  const otherTitle = await otherNode.locator(".graph-node-label").innerText();
  await otherNode.click();
  await expect(page.locator('.graph-node[data-depth="0"] .graph-node-label')).toHaveText(otherTitle);

  // Switching to List with a page centred in the graph is the one, consistent way to read it
  // (Knowledge.tsx's `view === "List" && open` rule) — ties graph navigation back to the full
  // reading page rather than a column beside the graph.
  await page.getByRole("button", { name: "List", exact: true }).click();
  await expect(page.locator(".kn-reader .detail-title")).toBeVisible();
});

test("graph shows the whole-vault global graph until a page is open", async ({ page }) => {
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  await expect(page.locator(".global-graph")).toBeVisible();
  // At least one scope panel and one node button render without any page open.
  await expect(page.locator(".global-panel").first()).toBeVisible();
  await expect(page.locator(".global-node").first()).toBeVisible();
});

test("clicking a global graph node opens that page", async ({ page }) => {
  // Opening a page from the global graph stays in Graph view (it does not on its own switch
  // to List and show the reading page — see criterion 8 above); what it does is set that page
  // as the ego graph's new centre, replacing the whole-vault view with its bounded
  // neighbourhood (spec §6.7 F-61). A `data-depth="0"` node is the centre.
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  await page.locator('.global-node[data-isolated="false"]').first().click();
  await expect(page.getByRole("img", { name: /relation graph/i })).toBeVisible();
  await expect(page.locator('.graph-node[data-depth="0"]')).toBeVisible();
});

test("a global graph node is keyboard-reachable and opens the page on Enter", async ({ page }) => {
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  const node = page.locator('.global-node[data-isolated="false"]').first();
  await node.focus();
  await expect(node).toBeFocused();
  await expect(node).toHaveClass(/global-node/);
  await page.keyboard.press("Enter");
  await expect(page.locator('.graph-node[data-depth="0"]')).toBeVisible();
});

test("historical pages are hidden until the toggle is on", async ({ page }) => {
  const before = await page.locator(".page-row").count();
  await page.getByRole("checkbox", { name: /Show historical/ }).check();
  // Checking the box refetches; counting straight after races the response and the
  // re-render. Poll until the list actually grows rather than asserting on whatever
  // happened to be mounted at that instant.
  await expect
    .poll(async () => page.locator(".page-row").count(), { timeout: 10_000 })
    .toBeGreaterThan(before);
});

test("the whole screen is reachable by keyboard", async ({ page }) => {
  // Wait for the list before tabbing. Without this the run starts against a half-mounted
  // screen whose focusable set is much smaller, so a fixed number of Tab presses can walk
  // off the end of the document and into browser chrome — where `:focus-visible` matches
  // nothing and the test fails. It passed in isolation and failed roughly two runs in
  // three as part of the file, which is the signature of a racing start state rather than
  // a real regression.
  await expect(page.locator(".page-row").first()).toBeVisible();

  // Assert the property the test is named for — that tabbing keeps landing on real,
  // visibly-focused controls inside the app — rather than that one magic number of presses
  // happens to end somewhere focusable. Any element reached must show a focus ring, which
  // is what makes the screen actually navigable rather than merely focusable.
  const seen = new Set<string>();
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press("Tab");
    const focused = page.locator(":focus");
    await expect(focused).toBeVisible();
    await expect(page.locator(":focus-visible")).toBeVisible();
    seen.add(await focused.evaluate((el) => el.tagName + ":" + (el.className || "")));
  }
  // Tab must move focus, not sit on one element: a dozen presses that never leave the
  // first control would satisfy every assertion above and still mean the screen is a trap.
  expect(seen.size).toBeGreaterThan(3);
});
