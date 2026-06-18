import { describe, it, expect, vi } from "vitest";
import { addMember, UnverifiedDevice } from "./addMember";

const KEY_A = new Uint8Array([1, 1]);
const KEY_B = new Uint8Array([2, 2]);

function deps(verified: Uint8Array[]) {
  return {
    conversations: {
      addUser: vi.fn(async () => ({ user_id: "bob", username: "bob", email: "b@c", role: "member" })),
      keyMaterial: vi.fn(async () => [
        { deviceId: "d1", signingPublicKey: KEY_A, keyPackage: new Uint8Array([10]) },
        { deviceId: "d2", signingPublicKey: KEY_B, keyPackage: new Uint8Array([20]) },
      ]),
      addDeviceToRoster: vi.fn(async () => {}),
      putGroupInfo: vi.fn(async () => {}),
    },
    crypto: {
      addMember: vi.fn(async () => ({ commit: new Uint8Array([99]), welcome: new Uint8Array([88]) })),
      exportGroupInfo: vi.fn(async () => new Uint8Array([7])),
    },
    kt: { verifyIdentity: vi.fn(async () => verified) },
    protocol: { sendCommit: vi.fn(), sendWelcome: vi.fn() },
    token: () => "TOK",
  };
}

describe("addMember", () => {
  it("KT-verifies, MLS-adds each device, rosters, delivers, publishes GroupInfo", async () => {
    const d = deps([KEY_A, KEY_B]);
    await addMember(d as any, { wsId: "w1", group: "g1", identity: "bob", userId: "bob-uuid", currentMaxSeq: 5 });
    expect(d.conversations.addUser).toHaveBeenCalledWith("TOK", "g1", "bob");
    expect(d.kt.verifyIdentity).toHaveBeenCalledWith("TOK", "bob-uuid");
    expect(d.crypto.addMember).toHaveBeenCalledTimes(2);
    expect(d.conversations.addDeviceToRoster).toHaveBeenCalledWith("TOK", "g1", "d1", 5);
    expect(d.conversations.addDeviceToRoster).toHaveBeenCalledWith("TOK", "g1", "d2", 5);
    expect(d.protocol.sendCommit).toHaveBeenCalledTimes(2);
    expect(d.protocol.sendWelcome).toHaveBeenCalledTimes(2);
    // GroupInfo is republished after EACH device add so it never lags the epoch.
    expect(d.conversations.putGroupInfo).toHaveBeenCalledTimes(2);
    expect(d.conversations.putGroupInfo).toHaveBeenCalledWith("TOK", "g1", new Uint8Array([7]));
  });

  it("REFUSES to add a device whose signing key is not KT-verified, granting NO server membership", async () => {
    const d = deps([KEY_A]); // KEY_B missing → d2 unverified
    await expect(addMember(d as any, { wsId: "w1", group: "g1", identity: "bob", userId: "bob-uuid", currentMaxSeq: 5 }))
      .rejects.toBeInstanceOf(UnverifiedDevice);
    expect(d.crypto.addMember).not.toHaveBeenCalled(); // refuse BEFORE any MLS add
    // verify-then-mutate: a KT-rejected user must NOT gain server-side membership.
    expect(d.conversations.addUser).not.toHaveBeenCalled();
  });

  it("verifies KT by user_id while addUser resolves by username (regression)", async () => {
    const d = deps([KEY_A, KEY_B]);
    await addMember(d as any, { wsId: "w1", group: "g1", identity: "bob", userId: "bob-uuid", currentMaxSeq: 5 });
    expect(d.kt.verifyIdentity).toHaveBeenCalledWith("TOK", "bob-uuid");
    expect(d.conversations.addUser).toHaveBeenCalledWith("TOK", "g1", "bob");
  });
});
