export type CryptoRequest =
  | { id: string; kind: "keyPackage" }
  | { id: string; kind: "signingPublicKey" }
  | { id: string; kind: "createGroup"; groupId: string }
  | { id: string; kind: "createGroupWithCompliance"; groupId: string; complianceKeyPackage: Uint8Array }
  | { id: string; kind: "addMember"; groupId: string; keyPackage: Uint8Array }
  | { id: string; kind: "removeMember"; groupId: string; leafIndex: number }
  | { id: string; kind: "joinFromWelcome"; welcome: Uint8Array }
  | { id: string; kind: "encrypt"; groupId: string; plaintext: Uint8Array }
  | { id: string; kind: "decrypt"; groupId: string; message: Uint8Array };

export type CryptoResponse =
  | { id: string; ok: true; result: unknown }
  | { id: string; ok: false; error: string };

export function isCryptoResponse(v: unknown): v is CryptoResponse {
  if (typeof v !== "object" || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.id === "string" && typeof o.ok === "boolean";
}
