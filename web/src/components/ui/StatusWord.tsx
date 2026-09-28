// Status is always rendered as a word, never colour alone (spec `02` §4): a
// superseded claim reads "superseded Jun 2026", not just a coloured dot.
//
// Callers decide whether to show it at all. "active" is the default state of nearly every
// page and claim, so the list and reader omit it (see isNotable) and print only the
// statuses that change how a reader should trust the text.
const HISTORICAL = new Set(["superseded", "resolved", "retired"]);

export function StatusWord({ status, asOf }: { status: string | null; asOf?: string | null }) {
  if (!status) return null;
  const historical = HISTORICAL.has(status);
  return (
    <span className="status-word" data-historical={historical ? "true" : "false"}>
      {status}
      {asOf ? ` ${formatMonth(asOf)}` : ""}
    </span>
  );
}

/** True when a status is worth printing: anything other than missing or the default "active". */
export function isNotable(status: string | null | undefined): boolean {
  return Boolean(status) && status !== "active";
}

export function formatMonth(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleDateString("en-GB", { month: "short", year: "numeric" });
}
