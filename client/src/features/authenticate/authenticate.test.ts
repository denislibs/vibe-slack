import { describe, it, expect, vi } from "vitest";
import { createAuthenticator, type OpaqueOpsLike, type AsLike } from "./authenticate";

const SERVER_ID_B64 = btoa("messenger-as");

function fakes() {
  const opaque: OpaqueOpsLike = {
    regInit: vi.fn(() => ({ flowId: "f1", request: "REQ" })),
    regFinalize: vi.fn(() => ({ record: "RECORD", exportKey: "EK" })),
    loginKE1: vi.fn(() => ({ flowId: "f2", ke1: "KE1" })),
    loginKE3: vi.fn(() => ({ ke3: "KE3", sessionKey: "SK" })),
  };
  const as: AsLike = {
    registerStart: vi.fn(async () => "RESP"),
    registerFinish: vi.fn(async (_email: string, _username: string, _record: string) => {}),
    loginStart: vi.fn(async () => ({ loginId: "L1", ke2: "KE2" })),
    loginFinish: vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: true })),
  };
  return { opaque, as };
}

describe("authenticate", () => {
  it("register runs init→start→finalize→finish in order", async () => {
    const { opaque, as } = fakes();
    await createAuthenticator(opaque, as).register("a@corp", "alice", "pw");
    expect(as.registerStart).toHaveBeenCalledWith("a@corp", "REQ");
    expect(opaque.regFinalize).toHaveBeenCalledWith("f1", "RESP", SERVER_ID_B64);
    expect(as.registerFinish).toHaveBeenCalledWith("a@corp", "alice", "RECORD");
  });

  it("login returns token + enroll flag", async () => {
    const { opaque, as } = fakes();
    const r = await createAuthenticator(opaque, as).login("a@corp", "pw");
    expect(as.loginStart).toHaveBeenCalledWith("a@corp", "KE1");
    expect(opaque.loginKE3).toHaveBeenCalledWith("f2", "KE2", SERVER_ID_B64);
    expect(r).toEqual({ sessionToken: "TOK", deviceEnrollRequired: true });
  });
});
