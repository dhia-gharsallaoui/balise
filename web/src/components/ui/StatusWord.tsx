// Status is always rendered as a word, never colour alone (spec `02` §4): a
// superseded claim reads "superseded Jun 2026", not just a coloured dot.
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

export function formatMonth(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleDateString("en-GB", { month: "short", year: "numeric" });
}
