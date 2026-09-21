import MarkdownIt from "markdown-it";

export type Block =
  | { kind: "para"; text: string }
  | { kind: "head"; text: string }
  | { kind: "list"; items: string[] }
  | { kind: "code"; text: string; lang: string };

const md = new MarkdownIt({ html: false });

export function parseBody(source: string): Block[] {
  const tokens = md.parse(source, {});
  const blocks: Block[] = [];
  let listItems: string[] | null = null;
  let pendingHeading = false;

  for (const token of tokens) {
    if (token.type === "heading_open") pendingHeading = true;
    else if (token.type === "bullet_list_open" || token.type === "ordered_list_open") listItems = [];
    else if (token.type === "bullet_list_close" || token.type === "ordered_list_close") {
      if (listItems?.length) blocks.push({ kind: "list", items: listItems });
      listItems = null;
    } else if (token.type === "fence") {
      blocks.push({ kind: "code", text: token.content, lang: token.info.trim() });
    } else if (token.type === "inline") {
      const text = plain(token.content);
      if (!text) continue;
      if (pendingHeading) { blocks.push({ kind: "head", text }); pendingHeading = false; }
      else if (listItems) listItems.push(text);
      else blocks.push({ kind: "para", text });
    }
  }
  return blocks;
}

function plain(source: string): string {
  return source
    .replace(/\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/(\*\*|__|\*|_|`)/g, "")
    .trim();
}
