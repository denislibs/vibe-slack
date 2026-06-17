// Use-case: add a user to an E2E (MLS) conversation with a HARD Key Transparency
// verification gate. Every one of the target identity's device signing keys MUST be
// present in the KT-verified set BEFORE that device is added to the group. The gate
// runs to completion before ANY MLS add, so an unverified device causes the whole
// operation to fail closed — no device gets added.

// Thrown when a target device's signing key is not in the KT-verified set.
export class UnverifiedDevice extends Error {
  constructor(deviceId: string) {
    super(`device ${deviceId} signing key is not KT-verified`);
    this.name = "UnverifiedDevice";
  }
}

export interface DeviceKeyMaterial {
  deviceId: string;
  signingPublicKey: Uint8Array;
  keyPackage: Uint8Array;
}

export interface ConversationsPort {
  addUser(token: string, group: string, emailOrUsername: string): Promise<unknown>;
  keyMaterial(token: string, wsId: string, identity: string): Promise<DeviceKeyMaterial[]>;
  addDeviceToRoster(token: string, group: string, deviceId: string, joinSeq: number): Promise<void>;
  putGroupInfo(token: string, group: string, bytes: Uint8Array): Promise<void>;
}

export interface CryptoPort {
  // The real CryptoClient.addMember returns a plain { commit, welcome } object; we keep
  // that shape here for testability. If a future engine returns getters, CM-7 wiring adapts.
  addMember(groupId: string, keyPackage: Uint8Array): Promise<{ commit: Uint8Array; welcome: Uint8Array }>;
  exportGroupInfo(groupId: string): Promise<Uint8Array>;
}

export interface KTPort {
  // Returns the identity's KT-verified device signing keys; throws on KT failure.
  verifyIdentity(token: string, identity: string): Promise<Uint8Array[]>;
}

// Minimal protocol port for the bytes we actually send. CM-7 wires these to the real
// ProtocolClient (commit -> group frame; welcome -> the new device).
export interface ProtocolPort {
  sendCommit(group: string, bytes: Uint8Array): void;
  sendWelcome(group: string, deviceId: string, bytes: Uint8Array): void;
}

export interface AddMemberDeps {
  conversations: ConversationsPort;
  crypto: CryptoPort;
  kt: KTPort;
  protocol: ProtocolPort;
  token(): string;
}

export interface AddMemberArgs {
  wsId: string;
  group: string;
  identity: string;
  currentMaxSeq: number;
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

function isVerified(key: Uint8Array, verified: Uint8Array[]): boolean {
  return verified.some((v) => bytesEqual(key, v));
}

export async function addMember(deps: AddMemberDeps, args: AddMemberArgs): Promise<void> {
  const { conversations, crypto, kt, protocol } = deps;
  const { wsId, group, identity, currentMaxSeq } = args;
  const token = deps.token();

  await conversations.addUser(token, group, identity);

  const verified = await kt.verifyIdentity(token, identity);
  const material = await conversations.keyMaterial(token, wsId, identity);

  // Hard KT gate: refuse the whole op before any MLS add if any device is unverified.
  for (const device of material) {
    if (!isVerified(device.signingPublicKey, verified)) {
      throw new UnverifiedDevice(device.deviceId);
    }
  }

  for (const device of material) {
    const { commit, welcome } = await crypto.addMember(group, device.keyPackage);
    await conversations.addDeviceToRoster(token, group, device.deviceId, currentMaxSeq);
    protocol.sendCommit(group, commit);
    protocol.sendWelcome(group, device.deviceId, welcome);
  }

  const gi = await crypto.exportGroupInfo(group);
  await conversations.putGroupInfo(token, group, gi);
}
