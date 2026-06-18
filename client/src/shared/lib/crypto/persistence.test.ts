import "fake-indexeddb/auto";
import { describe, it, expect } from "vitest";
import { loadCryptoState, saveCryptoState } from "./persistence";

describe("crypto state persistence", () => {
  it("returns null when nothing is stored", async () => {
    expect(await loadCryptoState("nobody@corp")).toBeNull();
  });

  it("round-trips a saved blob by device name", async () => {
    const blob = new Uint8Array([1, 2, 3, 4]);
    await saveCryptoState("alice@corp", blob);
    const got = await loadCryptoState("alice@corp");
    expect(got).not.toBeNull();
    expect(Array.from(got!)).toEqual([1, 2, 3, 4]);
  });

  it("keeps device states separate", async () => {
    await saveCryptoState("a@corp", new Uint8Array([1]));
    await saveCryptoState("b@corp", new Uint8Array([2]));
    expect(Array.from((await loadCryptoState("a@corp"))!)).toEqual([1]);
    expect(Array.from((await loadCryptoState("b@corp"))!)).toEqual([2]);
  });
});
