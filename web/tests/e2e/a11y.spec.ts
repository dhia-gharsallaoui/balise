import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

// Batch G Task 8. Axe scans in both themes with the wcag2a/wcag2aa/wcag21aa
// rule sets, per the task brief. Real violations are reported and fixed in
// tokens.css (never suppressed or downgraded here) — see batch-G-report.md.

for (const theme of ["light", "dark"] as const) {
  test(`no accessibility violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });

  test(`page detail has no violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    await page.locator(".page-open").first().click();
    const results = await new AxeBuilder({ page }).withTags(["wcag2aa"]).analyze();
    expect(results.violations).toEqual([]);
  });

  // Global graph view (no page open): the whole-vault "five islands" layout added for the
  // "show all well" request. Scanned separately from the default List view above, since that
  // scan never switches to Graph and would otherwise miss this surface entirely.
  test(`graph view (global, no page open) has no accessibility violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    await page.getByRole("button", { name: "Graph", exact: true }).click();
    await page.waitForSelector(".global-node");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });

  // Agents screen (02-ui-design-v1.md section 5.5). Scans whatever the real backend actually
  // returns — an empty state if there are no agents, the list+detail card otherwise — rather
  // than assuming a fixture, since this spec (unlike visual.spec.ts) targets the live dev
  // server + backend on :5173/:8099.
  test(`agents screen has no accessibility violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    // exact: true — the real vault has a Knowledge space literally named "Agents" (a
    // `.kn-space-row`, rendered as "Agents 7"), which a loose /^Agents/ match also picks up
    // alongside the Rail's own "Agents" button.
    await page.getByRole("button", { name: "Agents", exact: true }).click();
    await page.waitForSelector(".agents-screen, .empty");
    const firstRow = page.locator(".agents-row").first();
    if (await firstRow.count()) {
      await firstRow.click();
    }
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });

  // Sources screen (knowledge-v3.html lines 397-440). Scans the real backend's state — the
  // drop-zone form always renders (it needs no data), the ingest log may be empty or not —
  // rather than assuming a fixture, matching the Agents screen test above.
  test(`sources screen has no accessibility violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    await page.getByRole("button", { name: "Sources", exact: true }).click();
    await page.waitForSelector(".sources-page, .empty");
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });

  // Settings screen (02-ui-design-v1.md section 5.6). Scans the real backend's state across
  // all five tabs — Structure is the default and needs no click, the rest are cheap enough to
  // check in the same pass since none of them re-fetch data switching tabs just re-renders
  // what /api/settings already returned.
  test(`settings screen has no accessibility violations in ${theme}`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/");
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.waitForSelector(".settings-screen, .empty");
    for (const tabName of ["Scopes", "Sources and storage", "Models", "Export"]) {
      const tabButton = page.getByRole("tab", { name: tabName, exact: true });
      if (await tabButton.count()) {
        await tabButton.click();
      }
    }
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(results.violations).toEqual([]);
  });
}

// Deviation from task-8-brief.md's sample: the brief's reduced-motion test
// reads `.kn-mid`'s animationDuration, but this codebase has no `.kn-mid`
// element and no selector anywhere actually applies global.css's unused
// `rise` @keyframes (grep confirms it is dead CSS, wired up in no earlier
// batch). tokens.css's reduced-motion rule is real and global though —
// `*, *::before, *::after { animation-duration: 0ms !important;
// transition-duration: 0ms !important; }` — so this test exercises it
// against a real animated element instead: the graph view's `.graph-node`,
// whose `transition: opacity var(--duration) ease, border-color var(--duration)
// ease` (graph.css) is non-zero under normal motion. See batch-G-report.md.
test("reduced motion disables the graph node transition", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/");
  // The graph centres on the currently open page (spec §6.7 F-61) and shows an empty
  // state with no page open, so a page must be centred before it renders any `.graph-node`s.
  // Opening a page from List no longer works for this: List now shows the dedicated
  // full-width `.kn-reader` page (reading-view-report.md), which replaces the toolbar the
  // Graph button lives in — so this switches to Graph view first and centres the ego graph
  // by clicking a connected global node instead, exactly like knowledge.spec.ts's criterion 8.
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  await page.locator('.global-node[data-isolated="false"]').first().click();
  const node = page.locator(".graph-node").first();
  await expect(node).toBeVisible();
  const normalDuration = await node.evaluate((el) => getComputedStyle(el).transitionDuration);
  expect(normalDuration).not.toBe("0s");

  await page.emulateMedia({ reducedMotion: "reduce" });
  const reducedDuration = await node.evaluate((el) => getComputedStyle(el).transitionDuration);
  expect(reducedDuration).toBe("0s");
});

// Same check for the global graph's own node styling (.global-node's opacity transition
// in graph.css), which is separate CSS from the ego view's .graph-node and so is not
// covered by the test above.
test("reduced motion disables the global graph node transition", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/");
  await page.getByRole("button", { name: "Graph", exact: true }).click();
  const node = page.locator(".global-node").first();
  await expect(node).toBeVisible();
  const normalDuration = await node.evaluate((el) => getComputedStyle(el).transitionDuration);
  expect(normalDuration).not.toBe("0s");

  await page.emulateMedia({ reducedMotion: "reduce" });
  const reducedDuration = await node.evaluate((el) => getComputedStyle(el).transitionDuration);
  expect(reducedDuration).toBe("0s");
});
