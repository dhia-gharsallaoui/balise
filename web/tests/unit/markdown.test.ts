import { describe, expect, it } from "vitest";
import { parseBody } from "../../src/lib/markdown";

describe("parseBody", () => {
  it("turns a paragraph into a para block", () => {
    expect(parseBody("Hello there.")).toEqual([{ kind: "para", text: "Hello there." }]);
  });

  it("turns a heading into a head block", () => {
    expect(parseBody("## Current")).toEqual([{ kind: "head", text: "Current" }]);
  });

  it("turns a bullet list into one list block", () => {
    expect(parseBody("- one\n- two")).toEqual([{ kind: "list", items: ["one", "two"] }]);
  });

  it("keeps document order", () => {
    const blocks = parseBody("Intro.\n\n## Current\n\n- a\n\n## History\n\nEnd.");
    expect(blocks.map((b) => b.kind)).toEqual(["para", "head", "list", "head", "para"]);
  });

  it("renders a fenced code block as a code block", () => {
    expect(parseBody("```bash\necho hi\n```")).toEqual([
      { kind: "code", text: "echo hi\n", lang: "bash" },
    ]);
  });

  it("returns nothing for an empty body", () => {
    expect(parseBody("")).toEqual([]);
  });

  it("strips inline markdown from paragraph text", () => {
    expect(parseBody("A **bold** word")).toEqual([{ kind: "para", text: "A bold word" }]);
  });
});
