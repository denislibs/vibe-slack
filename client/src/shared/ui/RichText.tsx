import { For, type JSX } from "solid-js";
import { Dynamic } from "solid-js/web";

// Renders a message body. New messages are TipTap HTML; legacy messages are
// raw plaintext. Because received messages come from other clients over the
// wire, the HTML is UNTRUSTED — we never use innerHTML. Instead we parse it
// into an inert document and rebuild it from a strict allowlist, so only known
// tags/attributes ever reach the DOM (no scripts, no event handlers, no
// arbitrary URLs).

// Tag name (uppercase, as DOM reports) -> the element we emit.
const TAG_MAP: Record<string, string> = {
  P: "p", BR: "br", STRONG: "strong", B: "strong", EM: "em", I: "em",
  S: "s", DEL: "s", CODE: "code", PRE: "pre", UL: "ul", OL: "ol",
  LI: "li", BLOCKQUOTE: "blockquote",
};
// Elements whose contents must NOT be shown (executable / styling payloads).
const DROP_WHOLE = new Set(["SCRIPT", "STYLE", "TEMPLATE", "IFRAME", "OBJECT", "EMBED"]);

// A body looks like rich HTML only when it starts with a block tag TipTap emits.
// Anything else (legacy "hello", "fgdfgdf", a stray "<3") renders as plain text.
function isRichHtml(s: string): boolean {
  return /^\s*<(p|ul|ol|pre|blockquote|h[1-6])\b/i.test(s);
}

// Allow only http(s)/mailto links; everything else (javascript:, data:, …) is
// dropped. URL() parsing gives us the real scheme regardless of obfuscation.
function safeHref(raw: string | null): string | null {
  if (!raw) return null;
  try {
    const u = new URL(raw.trim());
    return ["http:", "https:", "mailto:"].includes(u.protocol) ? u.href : null;
  } catch {
    return null;
  }
}

function renderNode(node: ChildNode): JSX.Element {
  if (node.nodeType === 3 /* TEXT_NODE */) return node.textContent;
  if (node.nodeType !== 1 /* ELEMENT_NODE */) return null;
  const el = node as Element;
  if (DROP_WHOLE.has(el.tagName)) return null;

  const children = () => <For each={[...el.childNodes]}>{(c) => renderNode(c)}</For>;

  if (el.tagName === "A") {
    const href = safeHref(el.getAttribute("href"));
    // Unsafe link → unwrap, keep the visible text.
    if (!href) return children();
    return <a href={href} target="_blank" rel="noopener noreferrer">{children()}</a>;
  }

  const tag = TAG_MAP[el.tagName];
  if (tag === "br") return <br />;
  // Unknown/disallowed tag → unwrap it but keep its (allowlisted) children.
  if (!tag) return children();
  return <Dynamic component={tag}>{children()}</Dynamic>;
}

export function RichText(props: { source: string }): JSX.Element {
  if (!isRichHtml(props.source)) return <>{props.source}</>;
  // parseFromString builds an inert document: scripts don't run, resources
  // (e.g. <img src>) are not fetched, and we only re-emit allowlisted nodes.
  const doc = new DOMParser().parseFromString(props.source, "text/html");
  return <For each={[...doc.body.childNodes]}>{(n) => renderNode(n)}</For>;
}
