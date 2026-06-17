import { describe, it, expect, vi } from "vitest";
import { createAuthFlow } from "./authFlow";
import { createSessionStore } from "../entities/session/store";

function deps() {
  return {
    authenticator: {
      register: vi.fn(async () => {}),
      login: vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: true })),
    },
    onboard: vi.fn(async () => "DEV1"),
    connect: vi.fn(),
    session: createSessionStore(),
  };
}

describe("authFlow", () => {
  it("login → authenticated → onboard → onboarded → connect(token)", async () => {
    const d = deps();
    await createAuthFlow(d as any).login("a@corp", "pw");
    expect(d.session.status()).toBe("onboarded");
    expect(d.session.token()).toBe("TOK");
    expect(d.session.deviceId()).toBe("DEV1");
    expect(d.onboard).toHaveBeenCalledWith("TOK");
    expect(d.connect).toHaveBeenCalledWith("TOK");
  });

  it("skips onboarding when device_enroll_required is false", async () => {
    const d = deps();
    d.authenticator.login = vi.fn(async () => ({ sessionToken: "TOK", deviceEnrollRequired: false }));
    await createAuthFlow(d as any).login("a@corp", "pw");
    expect(d.onboard).not.toHaveBeenCalled();
    expect(d.connect).toHaveBeenCalledWith("TOK");
    expect(d.session.status()).toBe("authenticated");
  });
});
