import type {
  AgentActivityResponse, AgentsListResponse, AgentSummary, CreateAgentRequest,
  CreateAgentResponse, CreateScopeResponse, GraphResponse, HomeResponse, PageDetail, PageRef,
  PagesResponse, ReviewAcceptResult, ReviewClaimEdit, ReviewDetail, ReviewEditAcceptResult,
  ReviewListResponse, ReviewRejectResult, SearchResponse, SettingsResponse, SourcesLogResponse,
  SourcesUploadResult, TreeResponse,
} from "./types";

const BASE = import.meta.env.VITE_API_BASE ?? "";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

// Every call here carries the owner session cookie. `credentials: "include"` is required,
// not just harmless, for visual.spec.ts's deliberately cross-origin :5271 -> :8199 pair
// (see web/playwright.config.ts) — the default "same-origin" would silently drop the
// cookie on that pair and every request would 401. Same-origin callers (the normal
// :5173 -> :8099 dev pair, and production) are unaffected either way.
async function get<T>(path: string, params: Record<string, string> = {}): Promise<T> {
  const query = new URLSearchParams(params).toString();
  const url = `${BASE}${path}${query ? `?${query}` : ""}`;
  const response = await fetch(url, { credentials: "include" });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new ApiError(response.status, body.detail ?? response.statusText);
  }
  return response.json() as Promise<T>;
}

// post is the review screen's only write path (accept/reject) — small enough that a body of
// `{}` (accept takes none) or `{ reason }` (reject) covers every caller so far.
async function post<T>(path: string, body: unknown = {}): Promise<T> {
  const response = await fetch(`${BASE}${path}`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const responseBody = await response.json().catch(() => ({}));
    throw new ApiError(response.status, responseBody.detail ?? response.statusText);
  }
  return response.json() as Promise<T>;
}

export interface PageFilters { type?: string; tags?: string[]; historical?: boolean; }

function toParams(filters: PageFilters): Record<string, string> {
  const params: Record<string, string> = {};
  if (filters.type) params.type = filters.type;
  if (filters.tags?.length) params.tags = filters.tags.join(",");
  if (filters.historical) params.historical = "true";
  return params;
}

export const fetchTree = () => get<TreeResponse>("/api/tree");
export const fetchPages = (f: PageFilters) => get<PagesResponse>("/api/pages", toParams(f));
// A page is fetched by (scope, slug). The route used to be /api/pages/{slug}, which keyed on
// the slug alone and served whichever scope sorted first when two held that slug — one
// customer's body, claims and lint under another customer's row.
export const fetchPage = (ref: PageRef) =>
  get<PageDetail>(
    `/api/pages/${encodeURIComponent(ref.scope)}/${encodeURIComponent(ref.slug)}`,
  );
export const search = (q: string, historical = false) =>
  get<SearchResponse>("/api/search", historical ? { q, historical: "true" } : { q });
export const fetchGraph = (tags?: string[]) =>
  get<GraphResponse>("/api/graph", tags?.length ? { tags: tags.join(",") } : {});
export const fetchHome = () => get<HomeResponse>("/api/home");

export const fetchReview = () => get<ReviewListResponse>("/api/review");
export const fetchReviewItem = (id: string) =>
  get<ReviewDetail>(`/api/review/${encodeURIComponent(id)}`);
export const acceptReview = (id: string) =>
  post<ReviewAcceptResult>(`/api/review/${encodeURIComponent(id)}/accept`);
export const rejectReview = (id: string, reason: string) =>
  post<ReviewRejectResult>(`/api/review/${encodeURIComponent(id)}/reject`, { reason });
// editAcceptReview commits an edited change: `edits` names only the claims a reviewer retyped
// (added/reworded ones from the review detail's `after` list) before accepting — an empty
// array behaves exactly like a plain accept (internal/api/review.go's handleReviewEditAccept).
export const editAcceptReview = (id: string, edits: ReviewClaimEdit[]) =>
  post<ReviewEditAcceptResult>(`/api/review/${encodeURIComponent(id)}/edit-accept`, { edits });

export const fetchAgents = () => get<AgentsListResponse>("/api/agents");

export interface AgentActivityOptions { limit?: number; before?: number; }

export const fetchAgentActivity = (id: string, opts: AgentActivityOptions = {}) => {
  const params: Record<string, string> = {};
  if (opts.limit) params.limit = String(opts.limit);
  if (opts.before) params.before = String(opts.before);
  return get<AgentActivityResponse>(`/api/agents/${encodeURIComponent(id)}/activity`, params);
};

// createAgent mints a new agent token. The response's `token` field is the raw secret, shown
// exactly once — see CreateAgentResponse's own doc comment. Server-side validation (space
// names must already exist, capabilities must be one of read/remember/propose, no duplicate
// still-live name) is the real boundary; this call never trusts the caller's own checks.
export const createAgent = (req: CreateAgentRequest) =>
  post<CreateAgentResponse>("/api/agents", req);

// revokeAgent sets revoked_at on the token (never deletes the row — its audit history stays).
export const revokeAgent = (id: string) =>
  post<{ agent: AgentSummary }>(`/api/agents/${encodeURIComponent(id)}/revoke`);

export const fetchSourcesLog = () => get<SourcesLogResponse>("/api/sources/log");

export const fetchSettings = () => get<SettingsResponse>("/api/settings");

// createScope declares a new, empty space (add-only: no rename, no delete). The response's
// `scopes` list is the full, live, effective set immediately after the create.
export const createScope = (name: string) =>
  post<CreateScopeResponse>("/api/scopes", { name });

export interface SourcesUploadParams {
  scope: string;
  slug: string;
  // title is optional: an empty title lets the server derive one from the content's first
  // "# " heading, or fall back to the slug (internal/api/sources.go's deriveTitle).
  title?: string;
  content: string;
}

// uploadSourcePage cannot reuse the JSON-only `post` helper above: the drop zone's payload is
// the page's raw markdown/text content as the request body, not a JSON envelope — scope, slug
// and title travel as query parameters instead, matching POST /api/sources/pages exactly.
export async function uploadSourcePage(params: SourcesUploadParams): Promise<SourcesUploadResult> {
  const query: Record<string, string> = { scope: params.scope, slug: params.slug };
  if (params.title) query.title = params.title;
  const qs = new URLSearchParams(query).toString();
  const response = await fetch(`${BASE}/api/sources/pages?${qs}`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "text/plain; charset=utf-8" },
    body: params.content,
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new ApiError(response.status, body.detail ?? response.statusText);
  }
  return response.json() as Promise<SourcesUploadResult>;
}

// --- Owner auth -------------------------------------------------------------
//
// Mirrors internal/api/auth.go's authStatusResponse exactly (snake_case JSON tags, not
// camelCased) per this codebase's convention of matching Go field names directly rather
// than reshaping them at the boundary.
export interface AuthStatus {
  auth_required: boolean;
  authenticated: boolean;
}

// fetchAuthStatus and login intentionally bypass the `get`/`post` helpers above: a 401 from
// either is the expected "not logged in" answer, not an error to throw — callers need the
// body (and, for login, the status code) rather than a thrown ApiError.
export async function fetchAuthStatus(): Promise<AuthStatus> {
  const response = await fetch(`${BASE}/api/auth/status`, { credentials: "include" });
  return response.json() as Promise<AuthStatus>;
}

export async function login(password: string): Promise<AuthStatus> {
  const response = await fetch(`${BASE}/api/auth/login`, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new ApiError(response.status, body.detail ?? response.statusText);
  }
  return response.json() as Promise<AuthStatus>;
}

export async function logout(): Promise<AuthStatus> {
  return post<AuthStatus>("/api/auth/logout");
}
