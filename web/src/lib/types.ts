export type ClaimStatus = "active" | "superseded" | "resolved" | "retired" | "open";
export type TypeName =
  | "state" | "decision" | "gotcha" | "procedure" | "issue" | "incident" | "note" | "memory";

export interface Claim {
  text: string;
  status: ClaimStatus;
  as_of: string | null;
}

// A page is identified by (scope, slug), not by slug alone: /api/pages is unique on both,
// so the same slug can name a different customer's page in each scope. Everything that opens
// a page passes this pair.
export interface PageRef {
  scope: string;
  slug: string;
}

// Finding is one lint finding. `scope` is set only on a global finding — one attached to no
// document, which therefore has no document row to inherit a scope from.
export interface Finding {
  rule: string;
  severity: string;
  detail: string;
  action?: string;
  scope?: string;
}

export interface PageRow {
  slug: string;
  uid: string;
  title: string;
  type: TypeName;
  status: string | null;
  scope: string;
  space: string;
  historical: boolean;
  claims_count: number;
  tokens: number;
  owner: string | null;
  tags: string[];
  claims: Claim[];
}

export interface PageDetail extends PageRow {
  body_md: string;
  relations: Record<string, { slug: string; title: string; status: string | null }[]>;
  backlinks: { slug: string; title: string; status: string | null }[];
  sources: string[];
  lint: Finding[];
  last_verified: string | null;
}

export interface Group { type: TypeName; count: number; pages: PageRow[]; }
export interface PagesResponse { groups: Group[]; total: number; }

export interface SpaceNode { name: string; scope: string; count: number; children: SpaceNode[]; }
export interface TreeResponse {
  spaces: SpaceNode[];
  types: { name: TypeName; count: number }[];
  historical_count: number;
  // Findings that name no document — an unparseable page has no uid, so its finding can
  // never reach a page detail response. Optional because a server predating the field
  // simply omits it.
  global_lint?: Finding[];
}

export interface Hit {
  slug: string; title: string; type: TypeName; status: string | null;
  scope: string; matched_claims: string[]; why: string;
}
export interface SearchResponse { hits: Hit[]; coverage: "ok" | "low"; }

export interface GraphNode { uid: string; slug: string; title: string; type: TypeName; scope: string; }
export interface GraphEdge { from_uid: string; to_uid: string; kind: string; }
export interface GraphResponse { nodes: GraphNode[]; edges: GraphEdge[]; }

// Home screen (superseded from 02-ui-design-v1.md section 5.1's "briefing, not dashboard" to
// the card-grid direction in /tmp/balise-design/knowledge-v3.html, recorded in
// docs/superpowers/specs/2026-09-16-balise-vertical-slice-design.md section 9): still no
// invented numbers, but rendered as three cards instead of plain sentences. Every field here
// is either data the server already aggregated (attention[].sentence, owner) or the small set
// of facts needed to build a row client-side (waiting's proposals, changes' author word).

export interface HomeProposal {
  id: string;
  kind: string;
  scope: string;
  target: string;
  confidence: number;
  created_by: string;
  status: string;
}

export interface HomeKindCount { kind: string; count: number; }

export interface HomeWaiting {
  total: number;
  by_kind: HomeKindCount[];
  proposals: HomeProposal[];
}

// The page a "Needs attention" sentence links to. Reused as Knowledge's restrictTo filter, so
// it carries just enough to match a PageRow: scope + slug (title is display-only).
export interface HomePageRef {
  scope: string;
  slug: string;
  title: string;
}

export interface HomeAttention {
  rule: string;
  sentence: string;
  count: number;
  pages: HomePageRef[];
}

export interface HomeChange {
  sha: string;
  path: string;
  message: string;
  author_word: string;
  when: string;
  // Present together, or not at all: the backend only fills these in when the commit's path
  // resolves to a real indexed document (internal/api/home.go's resolveChangeRef). A change
  // with no scope is not navigable — e.g. a memory-import commit, a real tracked file the
  // indexer never turned into a page — and must render as plain text, not a broken link.
  scope?: string;
  slug?: string;
  title?: string;
}

export interface HomeResponse {
  waiting: HomeWaiting;
  attention: HomeAttention[];
  changes: HomeChange[];
  // The vault's most common page owner (computed server-side, never hardcoded), used to name
  // the greeting's recipient. Empty when no page carries an owner.
  owner: string;
}

// Review screen (02-ui-design-v1.md section 5.3). Confidence arrives pre-rendered as a word
// ("likely" | "possible" | "unsure") — the server owns that threshold, never a raw number the
// row could show as a percentage (section 5.3's explicit rule: "never a percentage in the row").
export type ConfidenceWord = "likely" | "possible" | "unsure";

// One row of the left-column queue.
export interface ReviewListItem {
  id: string;
  kind: string;
  scope: string;
  target: string;
  target_title: string;
  confidence: ConfidenceWord;
  created_by: string;
}

export interface ReviewListResponse { proposals: ReviewListItem[]; }

export interface ReviewEvidence {
  source: string;
  span?: [number, number];
  text: string;
}

// A claim in a before/after list, carrying the word the right column marks it with — the
// server decides "added" / "reworded" / "retired" / "unchanged", the UI only renders it.
export type ClaimMark = "unchanged" | "added" | "reworded" | "retired";

export interface ReviewClaim {
  id: string;
  text: string;
  status: string;
  as_of?: string;
  mark: ClaimMark;
}

export interface ReviewDetail {
  id: string;
  kind: string;
  scope: string;
  target: string;
  target_title: string;
  confidence: ConfidenceWord;
  created_by: string;
  evidence: ReviewEvidence[];
  before: ReviewClaim[];
  after: ReviewClaim[];
}

export interface ReviewAcceptResult {
  commit: string;
  index_stale: boolean;
  before: ReviewClaim[];
  after: ReviewClaim[];
}

export interface ReviewRejectResult {
  commit: string;
  path: string;
}

// POST /api/review/{id}/edit-accept's request body (04-technical-spec-v1.md section 13:
// "body = edited change"): one entry per claim the reviewer retyped before accepting. id must
// name a claim from the review detail's own `after` list — only one marked "added" or
// "reworded" is accepted; editing a page's untouched history is refused server-side.
export interface ReviewClaimEdit {
  id: string;
  text: string;
}

// A non-blocking flag on one edited claim — today only "over_word_limit", the exact code
// compile.ClaimWarning already uses for a freshly proposed over-length claim. Editing never
// becomes a stricter gate than proposing: this warns, it never blocks accept.
export interface ReviewClaimWarning {
  id: string;
  code: string;
}

export interface ReviewEditAcceptResult {
  commit: string;
  index_stale: boolean;
  before: ReviewClaim[];
  after: ReviewClaim[];
  warnings: ReviewClaimWarning[];
}

// Agents screen (02-ui-design-v1.md section 5.5). Mirrors internal/api/agents.go's
// agentSummary and activityRow exactly, field for field — the API package's own doc comment
// explains why those Go types are already named as if the copy rule applied to JSON keys too,
// so nothing here needs translating into a screen-shaped name.
export type AgentState = "active" | "revoked" | "expired";

export interface AgentSummary {
  id: string;
  name: string;
  scopes: string[];
  capabilities: string[];
  state: AgentState;
  created_at: string;
  last_used_at?: string | null;
}

export interface AgentsListResponse { agents: AgentSummary[]; }

export interface AgentActivityRow {
  id: number;
  when: string;
  tool: string;
  scopes: string[];
  query: string;
  result_count: number;
  latency_ms: number;
}

export interface AgentActivityResponse {
  agent_name: string;
  activity: AgentActivityRow[];
}

// POST /api/agents (internal/api/agents_write.go's createAgentRequest/createAgentResponse,
// field for field). expires_at, when present, must be an RFC 3339 timestamp string; omitting
// it (or capabilities) means "none" exactly as the Go side treats an absent JSON field.
export interface CreateAgentRequest {
  name: string;
  scopes: string[];
  capabilities?: string[];
  expires_at?: string;
}

// token is the raw, one-time secret: this response is the only place it is ever sent, and it
// is never retrievable again afterward — see agents_write.go's own doc comment.
export interface CreateAgentResponse {
  agent: AgentSummary;
  token: string;
}

// Sources screen (knowledge-v3.html lines 397-440). The upload result mirrors
// internal/api/sources.go's sourcesUploadResponse field for field. The ingest log reuses
// HomeChange rather than a new type — GET /api/sources/log returns exactly the same
// homeChangeJSON-shaped rows Home's "Changed recently" card already renders, because it is
// backed by the same s.homeChanges(ctx) call.
export interface SourcesUploadResult {
  commit: string;
  scope: string;
  slug: string;
  path: string;
  title: string;
  index_stale: boolean;
}

export interface SourcesLogResponse {
  ingests: HomeChange[];
}

// Settings screen (02-ui-design-v1.md section 5.6; layout from knowledge-v3.html lines
// 480-610). Mirrors internal/api/settings.go's settingsResponseJSON field for field — every
// number here is real, and Export has no corresponding type because it has no handler: there
// is nothing to fetch for a screen this build renders as an honest, disabled stub.
export interface SettingsType {
  name: TypeName;
  description: string;
  folder: string;
  count: number;
  fields: string[];
  // 0 means the type declares no staleness rule at all (incident, issue, note) — never a
  // fabricated placeholder standing in for "unknown".
  stale_after_days: number;
}

export interface SettingsTagGroup {
  name: string;
  top_level_count: number;
}

export interface SettingsTag {
  name: string;
  count: number;
}

export interface SettingsRelation {
  kind: string;
  count: number;
}

// agents is never null: internal/api/settings.go's settingsScopes always sends [] rather
// than omitting the field when no active agent can read a scope.
export interface SettingsScope {
  name: string;
  folder: string;
  is_client: boolean;
  count: number;
  agents: string[];
}

// Never carries a password, DB user, or anything else that could authenticate as this
// database — see settings.go's own doc comment. db_host/db_name identify where the vault's
// data lives, nothing more.
export interface SettingsSources {
  vault_path: string;
  is_git_repo: boolean;
  db_host: string;
  db_name: string;
  page_count: number;
  commit_count: number;
}

// api_key_set reports presence only, never ANTHROPIC_API_KEY's value. gateway_url is empty
// (and gateway_url_set is false) exactly when ANTHROPIC_BASE_URL is unset — never a
// fabricated vendor default.
export interface SettingsModels {
  gateway_url: string;
  gateway_url_set: boolean;
  api_key_set: boolean;
  note: string;
}

export interface SettingsResponse {
  types: SettingsType[];
  tag_groups: SettingsTagGroup[];
  tags: SettingsTag[];
  relations: SettingsRelation[];
  scopes: SettingsScope[];
  sources: SettingsSources;
  models: SettingsModels;
}

// POST /api/scopes (internal/api/scopes.go's createScopeRequest/createScopeResponse, field
// for field). Scope creation is add-only — there is no rename or delete request shape,
// because the API has neither.
export interface CreateScopeRequest {
  name: string;
}

// scopes is the full, live, effective scope list after the create — declared ∪ discovered —
// so a caller can render the new space as selectable immediately, before any page exists
// under it.
export interface CreateScopeResponse {
  name: string;
  scopes: string[];
}
