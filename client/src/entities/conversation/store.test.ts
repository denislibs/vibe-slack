import { describe, it, expect } from "vitest";
import { createConversationStore } from "./store";

describe("conversation store", () => {
  it("appends decrypted messages ordered by seq and tracks cursor", () => {
    const s = createConversationStore();
    s.addMessage("g1", { seq: 2, sender: "bob", text: "hi" });
    s.addMessage("g1", { seq: 1, sender: "alice", text: "yo" });
    const msgs = s.messages("g1");
    expect(msgs.map((m) => m.seq)).toEqual([1, 2]);
    expect(s.cursor("g1")).toBe(2);
  });

  it("dedupes by seq", () => {
    const s = createConversationStore();
    s.addMessage("g1", { seq: 1, sender: "a", text: "x" });
    s.addMessage("g1", { seq: 1, sender: "a", text: "x" });
    expect(s.messages("g1").length).toBe(1);
  });

  it("save is called on addMessage when persist is provided", () => {
    const saved: any[] = [];
    const store = createConversationStore({ load: async () => null, save: (b) => saved.push(JSON.parse(JSON.stringify(b))) });
    store.addMessage("g1", { seq: 1, sender: "a", text: "hi" });
    expect(saved.length).toBe(1);
    expect(saved[saved.length - 1].g1[0].text).toBe("hi");
  });

  it("hydrate loads persisted messages into the store", async () => {
    const store = createConversationStore({
      load: async () => ({ g1: [{ seq: 1, sender: "a", text: "from disk" }] }),
      save: () => {},
    });
    await store.hydrate();
    expect(store.messages("g1")).toEqual([{ seq: 1, sender: "a", text: "from disk" }]);
  });

  it("hydrate dedupes against existing messages by seq", async () => {
    const store = createConversationStore({
      load: async () => ({ g1: [{ seq: 1, sender: "a", text: "dup" }] }),
      save: () => {},
    });
    store.addMessage("g1", { seq: 1, sender: "a", text: "original" });
    await store.hydrate();
    expect(store.messages("g1").length).toBe(1);
    expect(store.messages("g1")[0].text).toBe("original"); // first-write wins via seq dedupe
  });
});
