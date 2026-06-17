// joinPublic: join a public channel by external commit. Fetches the published
// GroupInfo, derives an external-commit join, registers membership + device in
// the roster, and fans the commit out to the group so existing members merge it.
export class GroupInfoUnavailable extends Error {}

export interface JoinPublicDeps {
  conversations: {
    getGroupInfo(token: string, group: string): Promise<Uint8Array | null>;
    join(token: string, group: string): Promise<unknown>;
    addDeviceToRoster(token: string, group: string, deviceId: string, joinSeq: number): Promise<void>;
  };
  crypto: { joinByExternalCommit(groupInfo: Uint8Array): Promise<Uint8Array> };
  protocol: { sendCommit(group: string, bytes: Uint8Array): void };
  token: () => string;
  deviceId: () => string;
}

export async function joinPublic(
  deps: JoinPublicDeps,
  args: { group: string; currentMaxSeq: number },
): Promise<void> {
  const token = deps.token();
  const gi = await deps.conversations.getGroupInfo(token, args.group);
  if (!gi) throw new GroupInfoUnavailable(`no GroupInfo for ${args.group}`);
  const commit = await deps.crypto.joinByExternalCommit(gi);
  await deps.conversations.join(token, args.group);
  await deps.conversations.addDeviceToRoster(token, args.group, deps.deviceId(), args.currentMaxSeq);
  deps.protocol.sendCommit(args.group, commit);
}
