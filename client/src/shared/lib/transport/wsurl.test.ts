import { describe, it, expect } from "vitest";
import { resolveWsUrl } from "./wsurl";

describe("resolveWsUrl", () => {
  it("derives same-origin ws:// from an http origin", () => {
    expect(resolveWsUrl(undefined, { protocol: "http:", host: "localhost:5173" })).toBe("ws://localhost:5173/ws");
  });
  it("derives wss:// from an https origin", () => {
    expect(resolveWsUrl("", { protocol: "https:", host: "app.example" })).toBe("wss://app.example/ws");
  });
  it("honors an explicit override", () => {
    expect(resolveWsUrl("ws://other/ws", { protocol: "http:", host: "x" })).toBe("ws://other/ws");
  });
});
