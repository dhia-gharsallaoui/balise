/* web/src/lib/urlState.ts — the subset of app state that belongs in the address bar.

   Everything here is state a person would point at: which section they are on, what they
   searched for, which space and types they narrowed to, and which page they have open. Put
   together, those are what makes a link worth sending to someone. State that is a property
   of the viewport or the session rather than of what is being looked at — the breakpoint,
   the theme, an in-flight error, the transient page-list restriction Home hands to Knowledge
   when you click a "Needs attention" sentence — deliberately stays out.

   The parse/serialise pair is pure so it can be tested without a DOM; the three functions at
   the bottom are the only ones that touch `window`. Writers always merge over `current()`
   rather than constructing a whole URL from their own slice, so two components writing
   different params in the same tick cannot clobber each other. */

export type ViewMode = "List" | "Graph";

export interface PageKey {
  scope: string;
  slug: string;
}

export interface UrlParams {
  /** Lowercased section path segment ("home", "knowledge", …), or null for the default. */
  section: string | null;
  query: string;
  space: string | null;
  view: ViewMode;
  page: PageKey | null;
  /** Type names the viewer has switched OFF. Empty means "showing everything". */
  hiddenTypes: string[];
  historical: boolean;
}

export const DEFAULT_PARAMS: UrlParams = {
  section: null,
  query: "",
  space: null,
  view: "List",
  page: null,
  hiddenTypes: [],
  historical: false,
};

/** A page is addressed as `<scope>/<slug>`. Both are single path segments — `vault.
 *  ValidateSegment` rejects separators in either — so the first "/" is unambiguously the
 *  boundary, and a value with no "/" (or an empty half) is not a page reference at all. */
function parsePage(raw: string | null): PageKey | null {
  if (!raw) return null;
  const cut = raw.indexOf("/");
  if (cut <= 0 || cut === raw.length - 1) return null;
  return { scope: raw.slice(0, cut), slug: raw.slice(cut + 1) };
}

function formatPage(page: PageKey | null): string | null {
  if (!page || !page.scope || !page.slug) return null;
  return `${page.scope}/${page.slug}`;
}

/**
 * Reads params out of a location. Unknown keys, malformed values and unknown sections are
 * ignored rather than rejected: a URL someone edited by hand, or that outlived the param it
 * names, should still open the app on something sensible instead of an error.
 */
export function parseUrl(pathname: string, search: string): UrlParams {
  const q = new URLSearchParams(search);
  const segment = pathname.replace(/^\/+|\/+$/g, "").toLowerCase();
  const hidden = (q.get("hide") ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  return {
    section: segment === "" ? null : segment,
    query: q.get("q") ?? "",
    space: q.get("space") || null,
    view: q.get("view") === "Graph" ? "Graph" : "List",
    page: parsePage(q.get("page")),
    hiddenTypes: hidden,
    historical: q.get("historical") === "1",
  };
}

/**
 * Builds the path+query for a set of params, omitting anything at its default so a plain
 * view produces a plain URL. A link is likelier to be shared if it does not look alarming.
 */
export function buildUrl(p: UrlParams): string {
  const q = new URLSearchParams();
  if (p.query) q.set("q", p.query);
  if (p.space) q.set("space", p.space);
  if (p.view !== "List") q.set("view", p.view);
  const page = formatPage(p.page);
  if (page) q.set("page", page);
  if (p.hiddenTypes.length > 0) q.set("hide", [...p.hiddenTypes].sort().join(","));
  if (p.historical) q.set("historical", "1");

  // URLSearchParams percent-encodes "," and "/", turning readable values into
  // `hide=Gotcha%2CState` and `page=client-globex%2Fsome-slug`. Both characters are legal
  // unencoded inside a query string (RFC 3986 allows them in `query`), each escape has
  // exactly one meaning, and `parseUrl` reads either form — so decoding them back is
  // lossless in both directions, and a link someone is meant to paste into a message
  // looks like something a person wrote.
  const search = q.toString().replace(/%2C/g, ",").replace(/%2F/g, "/");
  return `/${p.section ?? ""}${search ? `?${search}` : ""}`;
}

function hasDom(): boolean {
  return typeof window !== "undefined" && typeof window.history !== "undefined";
}

/** The params the address bar currently holds. */
export function current(): UrlParams {
  if (!hasDom()) return { ...DEFAULT_PARAMS };
  return parseUrl(window.location.pathname, window.location.search);
}

/**
 * Writes params to the address bar.
 *
 * `replace` is not a detail: Knowledge searches on every keystroke, so pushing a history
 * entry per change would bury the previous screen under one entry per character and make
 * the back button useless. Continuous edits (typing, dragging a filter) replace; discrete
 * navigations (choosing a section, opening a page) push.
 */
export function write(patch: Partial<UrlParams>, replace = false): void {
  if (!hasDom()) return;
  const next = { ...current(), ...patch };
  const url = buildUrl(next);
  if (url === window.location.pathname + window.location.search) return;
  if (replace) window.history.replaceState(null, "", url);
  else window.history.pushState(null, "", url);
}

/** Subscribes to back/forward. Returns the unsubscribe so effects can clean up. */
export function onPopState(handler: (params: UrlParams) => void): () => void {
  if (!hasDom()) return () => {};
  const listener = () => handler(current());
  window.addEventListener("popstate", listener);
  return () => window.removeEventListener("popstate", listener);
}
