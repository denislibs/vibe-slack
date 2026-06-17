import { describe, it, expect, vi } from "vitest";
import { createConversation } from "./createConversation";

function deps(compliance: Uint8Array | null) {
  return {
    conversations: {
      create: vi.fn(async () => ({ group_id: "g1", type: "channel", visibility: "private", name: "team" })),
      complianceKeyPackage: vi.fn(async () => compliance),
      putGroupInfo: vi.fn(async () => {}),
    },
    crypto: {
      createGroup: vi.fn(async () => {}),
      createGroupWithCompliance: vi.fn(async () => new Uint8Array([9])), // compliance welcome
      exportGroupInfo: vi.fn(async () => new Uint8Array([1, 2, 3])),
    },
    token: () => "TOK",
  };
}

describe("createConversation", () => {
  it("creates server conv + MLS group WITH compliance, then publishes GroupInfo", async () => {
    const d = deps(new Uint8Array([7]));
    const conv = await createConversation(d as any, { wsId: "w1", type: "channel", visibility: "private", name: "team" });
    expect(conv.group_id).toBe("g1");
    expect(d.conversations.create).toHaveBeenCalledWith("TOK", "w1", { type: "channel", visibility: "private", name: "team", emailOrUsername: undefined });
    expect(d.crypto.createGroupWithCompliance).toHaveBeenCalledWith("g1", new Uint8Array([7]));
    expect(d.crypto.createGroup).not.toHaveBeenCalled();
    expect(d.conversations.putGroupInfo).toHaveBeenCalledWith("TOK", "g1", new Uint8Array([1, 2, 3]));
  });

  it("falls back to a plain group when compliance is not configured", async () => {
    const d = deps(null);
    await createConversation(d as any, { wsId: "w1", type: "dm", emailOrUsername: "bob" });
    expect(d.crypto.createGroup).toHaveBeenCalledWith("g1");
    expect(d.crypto.createGroupWithCompliance).not.toHaveBeenCalled();
    expect(d.conversations.create).toHaveBeenCalledWith("TOK", "w1", { type: "dm", visibility: undefined, name: undefined, emailOrUsername: "bob" });
    expect(d.conversations.putGroupInfo).toHaveBeenCalled();
  });
});
