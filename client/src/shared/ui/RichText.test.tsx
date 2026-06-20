// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect } from "vitest";
import { RichText } from "./RichText";

describe("RichText", () => {
  it("renders legacy plain text unchanged (no HTML wrapper)", () => {
    const { container } = render(() => <RichText source="hello world" />);
    expect(container.textContent).toBe("hello world");
    expect(container.querySelector("p")).toBeNull();
  });

  it("renders a paragraph with bold + italic + inline code", () => {
    const { container } = render(() => (
      <RichText source="<p>a <strong>b</strong> <em>c</em> <code>d</code></p>" />
    ));
    expect(container.querySelector("p")).toBeTruthy();
    expect(container.querySelector("strong")?.textContent).toBe("b");
    expect(container.querySelector("em")?.textContent).toBe("c");
    expect(container.querySelector("code")?.textContent).toBe("d");
  });

  it("renders bullet and ordered lists, blockquote, and code blocks", () => {
    const { container } = render(() => (
      <RichText source="<ul><li>one</li><li>two</li></ul><blockquote><p>q</p></blockquote><pre><code>x=1</code></pre>" />
    ));
    expect(container.querySelectorAll("ul li").length).toBe(2);
    expect(container.querySelector("blockquote")).toBeTruthy();
    expect(container.querySelector("pre code")?.textContent).toBe("x=1");
  });

  it("keeps safe https/mailto links and adds noopener", () => {
    const { container } = render(() => (
      <RichText source='<p><a href="https://example.com">x</a></p>' />
    ));
    const a = container.querySelector("a");
    expect(a?.getAttribute("href")).toBe("https://example.com/");
    expect(a?.getAttribute("target")).toBe("_blank");
    expect(a?.getAttribute("rel")).toContain("noopener");
  });

  it("strips javascript: links but keeps the visible text", () => {
    const { container } = render(() => (
      <RichText source={'<p><a href="javascript:alert(1)">click</a></p>'} />
    ));
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("click");
  });

  it("never emits <script> and never renders its source as text", () => {
    const { container } = render(() => (
      <RichText source={'<p>safe</p><script>window.__xss=1</script>'} />
    ));
    expect(container.querySelector("script")).toBeNull();
    expect(container.textContent).toBe("safe");
    expect((window as unknown as { __xss?: number }).__xss).toBeUndefined();
  });

  it("drops disallowed elements (img) and event-handler attributes", () => {
    const { container } = render(() => (
      <RichText source={'<p onclick="alert(1)">hi<img src=x onerror="alert(1)"></p>'} />
    ));
    expect(container.querySelector("img")).toBeNull();
    const p = container.querySelector("p");
    expect(p?.getAttribute("onclick")).toBeNull();
    expect(container.textContent).toBe("hi");
  });
});
