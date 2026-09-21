import type { ReactNode } from "react";

// hint accepts a ReactNode, not just a string, so a caller (e.g. the Agents screen's
// no-agents state) can embed an inline <code> snippet of a literal command a reader would
// otherwise have to retype from memory — every existing caller already passes a plain
// string, which is itself a valid ReactNode, so this is a widening, not a breaking change.
export function EmptyState({ title, hint }: { title: string; hint?: ReactNode }) {
  return (
    <div className="empty">
      <div className="empty-mark" aria-hidden="true" />
      <p className="empty-title">{title}</p>
      {hint ? <p className="empty-hint">{hint}</p> : null}
    </div>
  );
}
