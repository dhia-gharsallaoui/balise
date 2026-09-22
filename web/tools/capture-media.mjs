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
 * Requires ffmpeg and gifsicle on PATH for the GIFs; screenshots need neither.
 */
import { chromium } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync, readdirSync, existsSync } from "node:fs";
import path from "node:path";

const BASE = process.env.CAPTURE_BASE ?? "http://localhost:5173";
const PASSWORD = process.env.CAPTURE_PASSWORD ?? "demo";
const OUT = path.resolve("../docs/media");
const TMP = "/tmp/balise-capture";
const VIEW = { width: 1440, height: 900 };
// GIFs record at a smaller viewport than the screenshots so the downscale to GIF_WIDTH is
// gentle — resampling antialiased text hard, then quantising it to 256 colours, is what
// turns a crisp UI into mush.
const GIF_VIEW = { width: 1280, height: 800 };
const GIF_WIDTH = 1000;
const GIF_FPS = 10;

const sh = (cmd, args) => execFileSync(cmd, args, { stdio: ["ignore", "pipe", "pipe"] });

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
 * Refuses to capture anything unless this is demonstrably the demo vault. Runs once
 * against an already-navigated page — a relative fetch needs an origin, and a fresh
 * context sits on about:blank until something navigates it.
 */
async function assertDemoVault(page) {
  if (new URL(page.url()).protocol === "about:") await page.goto(`${BASE}/`);
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
  const ctx = await browser.newContext({
    viewport: GIF_VIEW,
    storageState: session,
    recordVideo: { dir, size: GIF_VIEW },
  });
  const page = await ctx.newPage();
  await go(page);
  await ctx.close();

  const webm = path.join(dir, readdirSync(dir).find((f) => f.endsWith(".webm")));
  const pal = path.join(dir, "palette.png");
  const raw = path.join(dir, "raw.gif");
  const out = path.join(OUT, `${name}.gif`);
  const filters = `fps=${GIF_FPS},scale=${GIF_WIDTH}:-1:flags=lanczos`;

  // Two passes: a palette built from the whole clip, then applied. One-pass GIF encoding
  // picks a palette per frame and makes text shimmer between frames.
  //
  // `dither=none` is the setting that matters. Dithering scatters pixels to fake colours a
  // 256-entry palette does not have, which suits photographs and ruins UI: on flat panels
  // and antialiased text it reads as speckle, and the first version of these GIFs looked
  // visibly pixelated because of it. A UI has few enough real colours that the palette can
  // just hold them. `--lossy=20` then halves the file with no visible cost; the original
  // `--lossy=60` was the other half of the damage.
  sh("ffmpeg", ["-loglevel", "error", "-y", "-i", webm, "-vf", `${filters},palettegen=stats_mode=diff`, pal]);
  sh("ffmpeg", ["-loglevel", "error", "-y", "-i", webm, "-i", pal,
    "-lavfi", `${filters}[x];[x][1:v]paletteuse=dither=none`, raw]);
  sh("gifsicle", ["-O3", "--lossy=20", raw, "-o", out]);
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
      await p.goto(`${BASE}/knowledge?view=Graph`); await p.locator(".global-node").first().waitFor(); await pause(p, 1800);
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
  await gif(browser, session, { name: "tour", go: async (p) => {
    await p.goto(`${BASE}/knowledge`); await ready(p); await pause(p, 1200);
    await p.getByPlaceholder("Search claims…").pressSequentially("failover", { delay: 110 });
    await pause(p, 1800);
    await p.locator(".page-open").first().click();
    await p.locator(".kn-reader").waitFor(); await pause(p, 2200);
    await p.goBack(); await ready(p); await pause(p, 700);
    // Clear the query before the graph: Graph honours the same filters as List, so leaving
    // "failover" in the box narrows it to two pages and there is nothing to look at.
    await p.getByPlaceholder("Search claims…").fill(""); await pause(p, 900);
    await p.locator(".kn-viewmode button", { hasText: "Graph" }).click();
    await p.locator(".global-node").first().waitFor({ timeout: 20_000 }); await pause(p, 2600);
  }});

  // The thesis: an agent proposed something, a human decides. Nothing is applied until
  // someone accepts it, and that is the whole point.
  await gif(browser, session, { name: "review", go: async (p) => {
    await p.goto(`${BASE}/review`); await pause(p, 2400);
    const accept = p.getByRole("button", { name: /^Accept/ }).first();
    if (await accept.count()) { await accept.click(); await pause(p, 2200); }
  }});

  await browser.close();
  console.log(`\nwrote ${readdirSync(OUT).length} files to docs/media/`);
}

await main();
