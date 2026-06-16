import type { CryptoRequest } from "./protocol";
import { isCryptoResponse } from "./protocol";

type Pending = { resolve: (v: unknown) => void; reject: (e: Error) => void };

// Omit distributes over each union member so per-kind fields are preserved
// (a plain Omit<CryptoRequest, "id"> would collapse to the common keys only).
type DistributiveOmit<T, K extends keyof any> = T extends unknown ? Omit<T, K> : never;
type CryptoRequestNoId = DistributiveOmit<CryptoRequest, "id">;

export class CryptoClient {
  private seq = 0;
  private pending = new Map<string, Pending>();

  constructor(private worker: Worker, name: string) {
    this.worker.onmessage = (ev: MessageEvent) => {
      const data = ev.data;
      if (!isCryptoResponse(data)) return;
      const p = this.pending.get(data.id);
      if (!p) return;
      this.pending.delete(data.id);
      if (data.ok) p.resolve(data.result);
      else p.reject(new Error(data.error));
    };
    // Send the device name once so the worker can build its engine.
    (this.worker as unknown as { postMessage: (m: unknown) => void }).postMessage({
      id: "init",
      kind: "keyPackage",
      name,
    });
  }

  private send(req: CryptoRequestNoId): Promise<unknown> {
    const id = `r${++this.seq}`;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.worker.postMessage({ ...req, id });
    });
  }

  async keyPackage(): Promise<Uint8Array> {
    return (await this.send({ kind: "keyPackage" })) as Uint8Array;
  }
  async createGroup(groupId: string): Promise<void> {
    await this.send({ kind: "createGroup", groupId });
  }
  async createGroupWithCompliance(groupId: string, complianceKeyPackage: Uint8Array): Promise<Uint8Array> {
    return (await this.send({ kind: "createGroupWithCompliance", groupId, complianceKeyPackage })) as Uint8Array;
  }
  async removeMember(groupId: string, leafIndex: number): Promise<Uint8Array> {
    return (await this.send({ kind: "removeMember", groupId, leafIndex })) as Uint8Array;
  }
  async addMember(groupId: string, keyPackage: Uint8Array) {
    return (await this.send({ kind: "addMember", groupId, keyPackage })) as {
      commit: Uint8Array;
      welcome: Uint8Array;
    };
  }
  async joinFromWelcome(welcome: Uint8Array): Promise<void> {
    await this.send({ kind: "joinFromWelcome", welcome });
  }
  async encrypt(groupId: string, plaintext: Uint8Array): Promise<Uint8Array> {
    return (await this.send({ kind: "encrypt", groupId, plaintext })) as Uint8Array;
  }
  async decrypt(groupId: string, message: Uint8Array): Promise<Uint8Array> {
    return (await this.send({ kind: "decrypt", groupId, message })) as Uint8Array;
  }
}
