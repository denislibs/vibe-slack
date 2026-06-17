import { describe, it, expect, vi } from "vitest";
import { onboardDevice, type CryptoLike, type EnrollLike } from "./onboardDevice";

function b64(bytes: number[]) { return btoa(String.fromCharCode(...bytes)); }

describe("onboardDevice", () => {
  it("collects signing key + key packages and enrolls, returning device id", async () => {
    const crypto: CryptoLike = {
      signingPublicKey: vi.fn(async () => new Uint8Array([1, 2, 3])),
      keyPackage: vi.fn(async () => new Uint8Array([9])),
    };
    const enroll: EnrollLike = {
      enrollDevice: vi.fn(async (_t, pub, label, kps) => {
        expect(pub).toBe(b64([1, 2, 3]));
        expect(label.length).toBeGreaterThan(0);
        expect(kps.length).toBe(3);
        return "DEVICE-1";
      }),
    };
    const id = await onboardDevice({ crypto, enroll, token: "TOK", label: "web", poolSize: 3 });
    expect(id).toBe("DEVICE-1");
    expect(crypto.keyPackage).toHaveBeenCalledTimes(3);
  });
});
