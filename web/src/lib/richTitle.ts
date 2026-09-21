// Titles are stored as plain strings, but 22/138 vault titles use backtick pairs to mark an
// identifier, path, or operator (e.g. `` AMW does not anchor PromQL `=~`; ... ``). The stored
// title is correct as-is — this module only concerns the *display* layer: it splits a title
// into alternating plain-text/code segments so a renderer can show the code spans as `<code>`
// without ever touching the raw string used for aria-label, title=, document title, search, or
// sort (see RichTitle.tsx and each call site for that split).

export interface TitleSegment {
  text: string;
  code: boolean;
}

/**
 * Splits `title` on paired backticks: the 1st and 2nd backtick bound a code span, the 3rd and
 * 4th bound the next, and so on (left-to-right, greedy). An unpaired trailing backtick — and
 * everything from it onward — is never treated as code: it renders as literal plain text,
 * backtick included, exactly as stored. Never throws, never drops characters: joining every
 * returned segment's `text` back together always reproduces `title` verbatim.
 */
export function splitTitleSegments(title: string): TitleSegment[] {
  const positions: number[] = [];
  for (let i = 0; i < title.length; i++) {
    if (title[i] === "`") positions.push(i);
  }

  // Only whole pairs count; a leftover unpaired index is dropped from the split points, so the
  // text from that backtick to the end of the string stays one literal plain-text run.
  const pairCount = Math.floor(positions.length / 2);

  const segments: TitleSegment[] = [];
  let cursor = 0;
  for (let p = 0; p < pairCount; p++) {
    const open = positions[p * 2];
    const close = positions[p * 2 + 1];
    if (open > cursor) segments.push({ text: title.slice(cursor, open), code: false });
    segments.push({ text: title.slice(open + 1, close), code: true });
    cursor = close + 1;
  }
  if (cursor < title.length) segments.push({ text: title.slice(cursor), code: false });

  return segments;
}

/** Strips paired backticks entirely, keeping their contents as literal text (no `<code>` markup).
 * For the one display surface that can't nest an element inside its text — the graph node label,
 * an absolutely-positioned <span> whose sibling <svg> draws the edges, but which is still plain
 * text content, not an SVG <text> node, so it just can't carry a styled inline child cleanly
 * next to its own truncation/ellipsis CSS. Unpaired backticks are left untouched, same rule as
 * splitTitleSegments. */
export function stripBackticks(title: string): string {
  return splitTitleSegments(title)
    .map((seg) => seg.text)
    .join("");
}
