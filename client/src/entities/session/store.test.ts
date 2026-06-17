import { describe, it, expect } from "vitest";
import { createSessionStore } from "./store";

describe("session store", () => {
  it("transitions anonymous → authenticated → onboarded", () => {
    const s = createSessionStore();
    expect(s.status()).toBe("anonymous");
    expect(s.token()).toBe("");
    s.authenticated("TOK");
    expect(s.status()).toBe("authenticated");
    expect(s.token()).toBe("TOK");
    s.onboarded("DEV1");
    expect(s.status()).toBe("onboarded");
    expect(s.deviceId()).toBe("DEV1");
  });

  it("clear() resets to anonymous", () => {
    const s = createSessionStore();
    s.authenticated("TOK");
    s.clear();
    expect(s.status()).toBe("anonymous");
    expect(s.token()).toBe("");
  });
});
