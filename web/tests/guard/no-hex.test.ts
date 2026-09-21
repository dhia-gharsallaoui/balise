import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const SRC = join(__dirname, "../../src");
const ALLOWED = "styles/tokens.css";
const HEX = /#[0-9a-fA-F]{3,8}\b/;

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

describe("token discipline", () => {
  it("defines colours only in tokens.css", () => {
    const offenders = walk(SRC)
      .filter((p) => /\.(tsx?|css)$/.test(p) && !p.endsWith(ALLOWED))
      .filter((p) => HEX.test(readFileSync(p, "utf8")))
      .map((p) => p.replace(SRC, "src"));
    expect(offenders, `Colour literals belong in ${ALLOWED}. Offenders: ${offenders.join(", ")}`)
      .toEqual([]);
  });
});
