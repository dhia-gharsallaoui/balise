import { Fragment } from "react";
import { splitTitleSegments } from "../../lib/richTitle";

// Renders a title's backtick-delimited spans as <code>, everywhere a title is shown as rich
// text (Home's "Changed recently", Knowledge's page rows/detail/graph, Review's queue and
// detail). The raw title string keeps being used, unchanged, for aria-label, title=,
// document.title, and search/sort — only this visible text changes. Not for the graph node
// label: an SVG-adjacent element can't nest <code> cleanly, so that one call site uses
// stripBackticks (lib/richTitle.ts) instead of this component.
export function RichTitle({ title }: { title: string }) {
  const segments = splitTitleSegments(title);
  return (
    <>
      {segments.map((segment, index) => (
        <Fragment key={index}>
          {segment.code ? <code className="rich-code">{segment.text}</code> : segment.text}
        </Fragment>
      ))}
    </>
  );
}
