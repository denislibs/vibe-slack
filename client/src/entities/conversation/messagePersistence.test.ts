import "fake-indexeddb/auto";
import { describe, it, expect } from "vitest";
import { loadMessages, saveMessages } from "./messagePersistence";

describe("conversation message persistence", () => {
  it("returns null when nothing stored", async () => {
    expect(await loadMessages("nobody")).toBeNull();
  });
  it("round-trips the byGroup map", async () => {
    const data = { g1: [{ seq: 1, sender: "alice", text: "hi" }] };
    await saveMessages("alice", data);
    expect(await loadMessages("alice")).toEqual(data);
  });
});
