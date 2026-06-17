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
});
