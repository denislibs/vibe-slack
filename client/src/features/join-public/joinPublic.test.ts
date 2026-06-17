import { describe, it, expect, vi } from "vitest";
import { joinPublic, GroupInfoUnavailable, type JoinPublicDeps } from "./joinPublic";

function fakes(groupInfo: Uint8Array | null) {
  const calls: string[] = [];
  const conversations = {
    getGroupInfo: vi.fn(async (_t: string, _g: string) => {
      calls.push("getGroupInfo");
      return groupInfo;
    }),
    join: vi.fn(async (_t: string, _g: string) => {
      calls.push("join");
      return { ok: true };
    }),
    addDeviceToRoster: vi.fn(async (_t: string, _g: string, _d: string, _s: number) => {
      calls.push("addDeviceToRoster");
    }),
  };
  const crypto = {
    joinByExternalCommit: vi.fn(async (_gi: Uint8Array) => {
      calls.push("joinByExternalCommit");
      return new Uint8Array([7, 7, 7]);
    }),
  };
  const protocol = {
    sendCommit: vi.fn((_g: string, _b: Uint8Array) => {
      calls.push("sendCommit");
    }),
  };
  const deps: JoinPublicDeps = {
    conversations,
    crypto,
    protocol,
    token: () => "tok",
    deviceId: () => "dev-1",
  };
  return { deps, conversations, crypto, protocol, calls };
}

describe("joinPublic", () => {
  it("happy path: fetches GroupInfo, builds external commit, joins, rosters, fans out commit — in order", async () => {
    const f = fakes(new Uint8Array([1, 2, 3]));

    await joinPublic(f.deps, { group: "p1", currentMaxSeq: 42 });

    expect(f.conversations.getGroupInfo).toHaveBeenCalledWith("tok", "p1");
    expect(f.crypto.joinByExternalCommit).toHaveBeenCalledWith(new Uint8Array([1, 2, 3]));
    expect(f.conversations.join).toHaveBeenCalledWith("tok", "p1");
    expect(f.conversations.addDeviceToRoster).toHaveBeenCalledWith("tok", "p1", "dev-1", 42);
    expect(f.protocol.sendCommit).toHaveBeenCalledWith("p1", new Uint8Array([7, 7, 7]));

    expect(f.calls).toEqual([
      "getGroupInfo",
      "joinByExternalCommit",
      "join",
      "addDeviceToRoster",
      "sendCommit",
    ]);
  });

  it("throws GroupInfoUnavailable and does nothing else when GroupInfo is null", async () => {
    const f = fakes(null);

    await expect(joinPublic(f.deps, { group: "p1", currentMaxSeq: 0 })).rejects.toBeInstanceOf(
      GroupInfoUnavailable,
    );

    expect(f.crypto.joinByExternalCommit).not.toHaveBeenCalled();
    expect(f.conversations.join).not.toHaveBeenCalled();
    expect(f.conversations.addDeviceToRoster).not.toHaveBeenCalled();
    expect(f.protocol.sendCommit).not.toHaveBeenCalled();
  });
});
