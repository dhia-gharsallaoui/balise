// Colour comes from CSS keyed on data-type (knowledge.css), reading the
// --type-* tokens — never a literal here.
export function TypeChip({ type }: { type: string }) {
  return (
    <span className="type-chip" data-type={type}>
      {type}
    </span>
  );
}
