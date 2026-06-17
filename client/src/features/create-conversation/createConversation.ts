import type { Conversation } from "../../shared/api/conversations";

export interface CreateConversationDeps {
  conversations: {
    create(token: string, wsId: string, body: { type: string; visibility?: string; name?: string; emailOrUsername?: string }): Promise<Conversation>;
    complianceKeyPackage(token: string): Promise<Uint8Array | null>;
    putGroupInfo(token: string, group: string, bytes: Uint8Array): Promise<void>;
  };
  crypto: {
    createGroup(groupId: string): Promise<void>;
    createGroupWithCompliance(groupId: string, complianceKeyPackage: Uint8Array): Promise<Uint8Array>;
    exportGroupInfo(groupId: string): Promise<Uint8Array>;
  };
  token: () => string;
}

export interface CreateConversationArgs {
  wsId: string;
  type: "dm" | "channel";
  visibility?: "public" | "private";
  name?: string;
  emailOrUsername?: string;
}

// Creates the server conversation, the local MLS group (with the visible compliance
// member when configured), and publishes the group's GroupInfo for external joins.
export async function createConversation(deps: CreateConversationDeps, args: CreateConversationArgs): Promise<Conversation> {
  const token = deps.token();
  const conv = await deps.conversations.create(token, args.wsId, {
    type: args.type, visibility: args.visibility, name: args.name, emailOrUsername: args.emailOrUsername,
  });
  const compliance = await deps.conversations.complianceKeyPackage(token);
  if (compliance) {
    await deps.crypto.createGroupWithCompliance(conv.group_id, compliance);
    // NOTE: the returned compliance Welcome must be delivered to the compliance
    // device via DS; delivery is wired in the add-member/orchestration task.
  } else {
    await deps.crypto.createGroup(conv.group_id);
  }
  const gi = await deps.crypto.exportGroupInfo(conv.group_id);
  await deps.conversations.putGroupInfo(token, conv.group_id, gi);
  return conv;
}
