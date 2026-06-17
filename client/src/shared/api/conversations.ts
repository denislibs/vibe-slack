type FetchFn = typeof fetch;

export interface Conversation {
  group_id: string;
  type: string;
  visibility: string;
  name: string;
}
export interface ConversationDetail extends Conversation {
  members: unknown[];
}
export interface ConversationMember {
  user_id: string;
  username: string;
  email: string;
  role: string;
}
export interface DeviceKeyMaterial {
  deviceId: string;
  signingPublicKey: Uint8Array;
  keyPackage: Uint8Array;
}
export interface CreateConversationInput {
  type: string;
  visibility?: string;
  name?: string;
  emailOrUsername?: string;
}

function b64ToBytes(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
function bytesToB64(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

// Typed client for the conversation / key-material / group-info / compliance
// endpoints. All calls carry the device-bound session token as a Bearer header.
export class ConversationsClient {
  // Default wraps fetch in an arrow so it's invoked unbound (calling native fetch as
  // a method, this.fetchFn(...), throws "Illegal invocation").
  constructor(private baseURL: string, private fetchFn: FetchFn = (...args) => fetch(...args)) {}

  private async call<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
    const init: RequestInit = {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      credentials: "include",
    };
    if (body !== undefined) init.body = JSON.stringify(body);
    const res = await this.fetchFn(this.baseURL + path, init);
    if (!res.ok) throw new Error(`conversations ${method} ${path} failed: ${res.status}`);
    return (await res.json()) as T;
  }

  // Variant that returns null on 404 (resource not yet published / configured).
  private async callOrNull<T>(token: string, method: string, path: string): Promise<T | null> {
    const res = await this.fetchFn(this.baseURL + path, {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      credentials: "include",
    });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error(`conversations ${method} ${path} failed: ${res.status}`);
    return (await res.json()) as T;
  }

  create(token: string, wsId: string, input: CreateConversationInput): Promise<Conversation> {
    const body: Record<string, string> = { type: input.type };
    if (input.visibility !== undefined) body.visibility = input.visibility;
    if (input.name !== undefined) body.name = input.name;
    if (input.emailOrUsername !== undefined) body.email_or_username = input.emailOrUsername;
    return this.call<Conversation>(token, "POST", `/workspaces/${encodeURIComponent(wsId)}/conversations`, body);
  }

  list(token: string, wsId: string): Promise<Conversation[]> {
    return this.call<Conversation[]>(token, "GET", `/workspaces/${encodeURIComponent(wsId)}/conversations`);
  }

  addUser(token: string, group: string, emailOrUsername: string): Promise<ConversationMember> {
    return this.call<ConversationMember>(token, "POST", `/conversations/${encodeURIComponent(group)}/users`, {
      email_or_username: emailOrUsername,
    });
  }

  join(token: string, group: string): Promise<{ ok: true }> {
    return this.call<{ ok: true }>(token, "POST", `/conversations/${encodeURIComponent(group)}/join`);
  }

  get(token: string, group: string): Promise<ConversationDetail> {
    return this.call<ConversationDetail>(token, "GET", `/conversations/${encodeURIComponent(group)}`);
  }

  // Register a device in the DS device roster for the group.
  addDeviceToRoster(token: string, group: string, deviceId: string, joinSeq: number): Promise<{ ok: true }> {
    return this.call<{ ok: true }>(token, "POST", `/conversations/${encodeURIComponent(group)}/members`, {
      device_id: deviceId,
      join_seq: joinSeq,
    });
  }
  // Alias matching the endpoint path name.
  members(token: string, group: string, deviceId: string, joinSeq: number): Promise<{ ok: true }> {
    return this.addDeviceToRoster(token, group, deviceId, joinSeq);
  }

  async keyMaterial(token: string, wsId: string, identity: string): Promise<DeviceKeyMaterial[]> {
    const j = await this.call<{ device_id: string; signing_public_key: string; key_package: string }[]>(
      token,
      "GET",
      `/workspaces/${encodeURIComponent(wsId)}/users/${encodeURIComponent(identity)}/key-material`,
    );
    return j.map((d) => ({
      deviceId: d.device_id,
      signingPublicKey: b64ToBytes(d.signing_public_key),
      keyPackage: b64ToBytes(d.key_package),
    }));
  }

  async getGroupInfo(token: string, group: string): Promise<Uint8Array | null> {
    const j = await this.callOrNull<{ group_info: string }>(
      token,
      "GET",
      `/conversations/${encodeURIComponent(group)}/group-info`,
    );
    return j === null ? null : b64ToBytes(j.group_info);
  }

  putGroupInfo(token: string, group: string, bytes: Uint8Array): Promise<{ ok: true }> {
    return this.call<{ ok: true }>(token, "PUT", `/conversations/${encodeURIComponent(group)}/group-info`, {
      group_info: bytesToB64(bytes),
    });
  }

  async complianceKeyPackage(token: string): Promise<Uint8Array | null> {
    const j = await this.callOrNull<{ key_package: string }>(token, "GET", "/keypackages/compliance");
    return j === null ? null : b64ToBytes(j.key_package);
  }
}
