/* web/tools/capture-media.mjs — regenerates every screenshot and GIF in the README.
 *
 * Committed rather than run ad-hoc so the media is reproducible: the demo vault is
 * generated deterministically (`make demo`), and this drives it through the same flows
 * every time, so a rerun produces the same pictures. That is the point of having it — a
 * screenshot nobody can regenerate goes stale the first time the UI moves and there is no
 * way to tell.
 *
 * Never point this at a real vault. Every frame it captures ends up in a public README,
 * and a vault with real content would put that content on the internet. It refuses to run
 * unless the backend reports the demo vault's own page count.
 *
 * Usage:
 *   make demo
 *   BALISE_PASSWORD=demo make up VAULT=demo-vault DEFAULTS=demo-vault-defaults \
 *     DSN='postgresql://...?search_path=demo_vault,public' PASSWORD=demo
 *   node tools/capture-media.mjs
 *
 * GIFs are rendered by `screencli export`, which gives auto-zoom on each action, click
 * highlights, a cursor trail, a padded background and rounded corners -- the polish a
 * hand-rolled ffmpeg pipeline does not have. screencli's own `record` command drives the
 * browser with an AI agent; we do not use it. The flow below is scripted, so it is
 * deterministic and nothing about the page is sent anywhere. We only borrow the renderer,
 * by writing the recording directory screencli's `export` expects: raw.webm plus an
 * events.json describing what was clicked and when. `export` needs no account and makes no
 * network call.
 *
 * Requires ffmpeg on PATH; screencli is fetched by npx on first use.
 */
import { chromium } from "@playwright/test";
// screencli's own compose step, called directly. `screencli export` only crops when the
// preset is a GIF — the auto-zoom, click ripple and cursor trail are baked in earlier, by
// this function, into a composed.mp4 that export then picks up. Calling it here is what
// gets the polish without handing the browser to their AI agent.
import { composeVideo } from "screencli/dist/src/video/compose.js";
import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync, readdirSync, existsSync, writeFileSync, renameSync } from "node:fs";
import path from "node:path";

const BASE = process.env.CAPTURE_BASE ?? "http://localhost:5173";
const PASSWORD = process.env.CAPTURE_PASSWORD ?? "demo";
const OUT = path.resolve("../docs/media");
const TMP = "/tmp/balise-capture";
const VIEW = { width: 1440, height: 900 };
// screencli's github-gif preset outputs 800x450 and crops toward wherever the events say
// the action was, so record at a 16:9 viewport to keep that crop predictable.
const GIF_VIEW = { width: 1280, height: 720 };
const SCREENCLI = "0.3.12";

const sh = (cmd, args) => execFileSync(cmd, args, { stdio: ["ignore", "pipe", "pipe"] });

/**
 * Records what the demo did, in the shape screencli's exporter reads.
 *
 * The zoom, the click ripple and the cursor trail are all driven by these events: each one
 * carries a bounding box, so the exporter knows where on screen the action happened and can
 * push the camera there. No events means a flat recording with no polish, which is exactly
 * what the previous pipeline produced.
 */
function makeRecorder(page, viewport) {
  const started = Date.now();
  const events = [];
  let id = 0;
  const at = () => Date.now() - started;

  async function push(type, description, locator, value) {
    let bounding_box;
    if (locator) {
      const box = await locator.boundingBox().catch(() => null);
      if (box) bounding_box = { x: Math.round(box.x), y: Math.round(box.y), width: Math.round(box.width), height: Math.round(box.height) };
    }
    events.push({ id: id++, timestamp_ms: at(), type, bounding_box, viewport, description, ...(value ? { value } : {}) });
  }

  return {
    events,
    elapsed: at,
    async click(locator, description) { await push("click", description, locator); await locator.click(); },
    async type(locator, text, description) {
      await push("type", description, locator, text);
      await locator.pressSequentially(text, { delay: 110 });
    },
    // Only click and type are emitted. screencli's compose step trims idle time and drives
    // the zoom from event bounding boxes; a boxless event (a navigate) at timestamp zero
    // produced a zero-length segment and a bare "FFmpeg exited with code 1". Navigation is
    // still performed, it just is not an event worth zooming to.
    async navigate(url) { await page.goto(url); },
  };
}

/**
 * Logs in once and returns the session cookies for every later context to reuse.
 *
 * Logging in per capture is the obvious thing and it does not work: /api/auth/login is
 * rate-limited (internal/api/auth.go), and a dozen captures in a row trip it and start
 * coming back 429. One login, reused — the same approach tests/e2e/global-setup.ts takes.
 */
async function signIn(browser) {
  const ctx = await browser.newContext({ viewport: VIEW });
  const page = await ctx.newPage();
  await page.goto(`${BASE}/`);
  const res = await page.evaluate(async (password) => {
    const r = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    return r.status;
  }, PASSWORD);
  if (res !== 200) throw new Error(`login failed with ${res} — is the backend up with PASSWORD=${PASSWORD}?`);
  const { cookies } = await ctx.storageState();
  await ctx.close();
  return { cookies, origins: [] };
}

/**
 * Refuses to capture anything unless this is demonstrably the demo vault, and unless that
 * vault still has proposals to show.
 *
 * The second check exists because this script is not read-only: the review clip clicks
 * Accept, which really accepts a proposal and commits it. Run the capture four times and
 * the demo vault's four proposals are gone — Home loses its "Waiting for you" card and the
 * run fails somewhere unrelated, looking like a UI bug. `make demo` is deterministic, so
 * regenerating restores them exactly.
 *
 * Runs once against an already-navigated page — a relative fetch needs an origin, and a
 * fresh context sits on about:blank until something navigates it.
 */
async function assertDemoVault(page) {
  if (new URL(page.url()).protocol === "about:") await page.goto(`${BASE}/`);
  const home = await page.evaluate(async () => (await fetch("/api/home")).json());
  if (!home.waiting?.total) {
    throw new Error(
      "refusing to capture: the demo vault has no proposals left. The review clip accepts " +
        "one on every run, so regenerate with `make demo` and restart the backend against it.",
    );
  }
  const tree = await page.evaluate(async () => (await fetch("/api/tree")).json());
  const scopes = new Set(tree.spaces.map((s) => s.scope));
  const expected = ["work", "client-acme", "client-globex", "client-initech"];
  const missing = expected.filter((s) => !scopes.has(s));
  if (missing.length > 0) {
    throw new Error(
      `refusing to capture: this is not the demo vault (missing scopes: ${missing.join(", ")}; ` +
        `found: ${[...scopes].join(", ")}). Run \`make demo\` and point the backend at it.`,
    );
  }
}

async function shot(browser, session, { name, theme, go }) {
  const ctx = await browser.newContext({ viewport: VIEW, colorScheme: theme, storageState: session });
  const page = await ctx.newPage();
  await go(page);
  await page.screenshot({ path: path.join(OUT, `${name}.png`) });
  await ctx.close();
  console.log(`  ${name}.png`);
}

async function gif(browser, session, { name, go }) {
  const dir = path.join(TMP, name);
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  const ctx = await browser.newContext({
    viewport: GIF_VIEW,
    storageState: session,
    recordVideo: { dir, size: GIF_VIEW },
  });
  const page = await ctx.newPage();
  const rec = makeRecorder(page, GIF_VIEW);
  await go(page, rec);
  const duration = rec.elapsed();
  await ctx.close();

  // Playwright names the video after the page; screencli expects raw.webm beside its
  // metadata, so move it into place rather than teaching the exporter a new path.
  const out = path.join(OUT, `${name}.gif`);
  const webm = path.join(dir, readdirSync(dir).find((f) => f.endsWith(".webm")));
  const raw = path.join(dir, "raw.webm");
  if (webm !== raw) renameSync(webm, raw);

  const eventsFile = path.join(dir, "events.json");
  writeFileSync(eventsFile, JSON.stringify(rec.events, null, 2));
  writeFileSync(path.join(dir, "metadata.json"), JSON.stringify({
    id: name,
    created_at: new Date().toISOString(),
    url: BASE,
    // Scripted, not agent-driven: this field exists because screencli's own recorder is an
    // AI agent following a prompt. Saying so keeps the recording honest about its origin.
    prompt: `scripted capture: ${name}`,
    model: "none (scripted)",
    viewport: GIF_VIEW,
    duration_ms: duration,
    raw_video_path: raw,
    event_log_path: eventsFile,
    chapters: [],
    agent_stats: { total_actions: rec.events.length, input_tokens: 0, output_tokens: 0 },
  }, null, 2));

  try {
  await composeVideo({
    rawVideoPath: raw,
    events: rec.events,
    outputPath: path.join(dir, "composed.mp4"),
    viewport: GIF_VIEW,
    // Zoom off, on purpose. screencli's auto-zoom crops to 40% of the viewport (a 2.5x
    // push) over a one-second approach before every action, and its timings are module
    // constants rather than options. On a dense two-pane UI that reads as the camera
    // lurching in and cutting off the panel you were just reading -- it hides context
    // instead of directing attention. The cursor and the click highlight already say where
    // the action is, without taking the rest of the screen away.
    zoom: false,
    highlight: true,
    cursor: true,
    preset: "fast",
  });
  } catch (e) {
    console.error(`compose failed for ${name}; recording kept at ${dir}`);
    console.error(JSON.stringify(rec.events, null, 1));
    throw e;
  }

  // export finds composed.mp4 on its own and prefers it over raw.webm.
  sh("npx", ["screencli", "export", dir, "--preset", "github-gif"]);

  const exported = readdirSync(path.join(dir, "exports")).find((f) => f.endsWith(".gif"));
  if (!exported) throw new Error(`screencli produced no gif for ${name}`);

  // One gifsicle pass after screencli. The auto-zoom means almost every frame differs from
  // the last, which defeats GIF's inter-frame compression -- one clip came out at 6MB,
  // past the 5MB a README should ask anyone to download. --lossy=20 roughly halves it and
  // is well short of the --lossy=60 that visibly smeared text in the earlier pipeline.
  sh("gifsicle", ["-O3", "--lossy=20", path.join(dir, "exports", exported), "-o", out]);
  rmSync(dir, { recursive: true, force: true });
  console.log(`  ${name}.gif`);
}

const ready = (page) => page.locator(".page-row").first().waitFor({ state: "visible", timeout: 20_000 });
const pause = (page, ms) => page.waitForTimeout(ms);

async function main() {
  if (!existsSync(OUT)) mkdirSync(OUT, { recursive: true });
  const browser = await chromium.launch();
  const session = await signIn(browser);

  // Once, before anything is written to disk.
  const guardCtx = await browser.newContext({ viewport: VIEW, storageState: session });
  const guardPage = await guardCtx.newPage();
  await assertDemoVault(guardPage);
  await guardCtx.close();

  console.log("screenshots:");
  for (const theme of ["light", "dark"]) {
    const s = theme === "light" ? "" : "-dark";
    await shot(browser, session, { name: `home${s}`, theme, go: async (p) => {
      await p.goto(`${BASE}/home`); await p.locator(".home-card").first().waitFor(); await pause(p, 900);
    }});
    await shot(browser, session, { name: `knowledge${s}`, theme, go: async (p) => {
      await p.goto(`${BASE}/knowledge`); await ready(p); await pause(p, 600);
    }});
    await shot(browser, session, { name: `graph${s}`, theme, go: async (p) => {
      await p.goto(`${BASE}/knowledge?view=Graph`); await p.locator(".cy-container canvas").first().waitFor();
      // The canvas mounts immediately; fcose then lays out asynchronously and the view is
      // fitted only once it settles. Waiting on the element alone captures nodes mid-flight.
      await pause(p, 4500);
    }});
    await shot(browser, session, { name: `review${s}`, theme, go: async (p) => {
      await p.goto(`${BASE}/review`); await pause(p, 1400);
    }});
  }
  await shot(browser, session, { name: "settings", theme: "light", go: async (p) => {
    await p.goto(`${BASE}/settings`); await pause(p, 1400);
  }});
  await shot(browser, session, { name: "reader", theme: "light", go: async (p) => {
    await p.goto(`${BASE}/knowledge`); await ready(p);
    await p.locator(".page-open").first().click();
    await p.locator(".kn-reader").waitFor(); await pause(p, 900);
  }});

  console.log("gifs:");
  // The hero: find something by claim, read it, see how it connects. The loop the product
  // exists for, in one take.
  await gif(browser, session, { name: "tour", go: async (p, rec) => {
    await rec.navigate(`${BASE}/knowledge`);
    await ready(p); await pause(p, 1200);
    await rec.type(p.getByPlaceholder("Search claims…"), "failover", "Search claims for failover");
    await pause(p, 1800);
    await rec.click(p.locator(".page-open").first(), "Open the matching page");
    await p.locator(".kn-reader").waitFor(); await pause(p, 2400);
    await p.goBack(); await ready(p); await pause(p, 700);
    // Clear the query before the graph: Graph honours the same filters as List, so leaving
    // "failover" in the box narrows it to two pages and there is nothing to look at.
    await p.getByPlaceholder("Search claims…").fill(""); await pause(p, 900);
    await rec.click(p.locator(".kn-viewmode button", { hasText: "Graph" }), "Switch to the relation graph");
    await p.locator(".cy-container canvas").first().waitFor({ timeout: 20_000 });
    await pause(p, 4200);
    // Zoom in, then fit. Two reasons. It shows the graph is navigable rather than a static
    // picture, and — less obviously — it keeps the segment at all: screencli's compose step
    // trims idle time, and a long settle pause with no events in it is idle by definition.
    // The first cut of this clip ended on an empty panel because the whole graph section
    // had been trimmed away.
    await rec.click(p.getByRole("button", { name: "Zoom in" }), "Zoom into the graph");
    await pause(p, 1600);
    await rec.click(p.getByRole("button", { name: "Fit graph to view" }), "Fit the whole graph");
    await pause(p, 2400);
  }});

  // The thesis: an agent proposed something, a human decides. Nothing is applied until
  // someone accepts it, and that is the whole point.
  await gif(browser, session, { name: "review", go: async (p, rec) => {
    await rec.navigate(`${BASE}/review`);
    await pause(p, 2600);
    const accept = p.getByRole("button", { name: /^Accept/ }).first();
    if (await accept.count()) {
      await rec.click(accept, "Accept the proposed claim");
      await pause(p, 2600);
    }
  }});

  await browser.close();
  console.log(`\nwrote ${readdirSync(OUT).length} files to docs/media/`);
}

await main();
