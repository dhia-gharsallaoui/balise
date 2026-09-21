/* web/src/lib/plural.ts — "3 pages", "1 page", "3 entries".

   This existed as three byte-identical copies, in Home.tsx, SourcesScreen.tsx and
   SettingsScreen.tsx, each appending a bare "s". That worked for every noun the screens
   happened to pass — page, commit, edge, proposal, item, value, day — right up until the
   Sources ingest log asked for "entry" and rendered "3 entrys" on screen. Three copies of a
   rule means the fix has to be found three times; one copy means the next irregular noun is
   handled everywhere at once. */

/**
 * Formats a count with its noun, pluralising the noun when the count is not exactly 1.
 *
 * Handles the consonant + "y" → "ies" rule ("entry" → "entries") while leaving vowel + "y"
 * alone ("day" → "days", not "daies"), and the sibilant endings that take "es" ("match" →
 * "matches"). Anything else takes a plain "s".
 *
 * This is deliberately not a general English pluraliser: the vault's screens name countable
 * concrete things, and an irregular noun ("person", "index") should be reworded or added
 * here explicitly rather than guessed at by a rule that will be wrong somewhere else.
 */
export function plural(count: number, unit: string): string {
  return `${count} ${count === 1 ? unit : pluralise(unit)}`;
}

/** The plural form of a noun on its own, for callers that render the count separately. */
export function pluralise(unit: string): string {
  if (/[^aeiou]y$/i.test(unit)) return `${unit.slice(0, -1)}ies`;
  if (/(s|x|z|ch|sh)$/i.test(unit)) return `${unit}es`;
  return `${unit}s`;
}
