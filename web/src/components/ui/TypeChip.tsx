// A type is a coloured mark plus an ink label, never coloured text: several --type-* hues fail
// contrast as text. The dot's colour comes from CSS keyed on data-type (knowledge.css), reading
// the --type-* tokens, never a literal here.
export function TypeChip({ type }: { type: string }) {
  return (
    <span className="type-chip" data-type={type}>
      <span className="type-dot" data-type={type} aria-hidden="true" />
      {type}
    </span>
  );
}
