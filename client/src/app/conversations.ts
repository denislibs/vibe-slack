import { createSignal } from "solid-js";

export interface ConvSummaryChannel { id: string; name: string; visibility: string; }
export interface ConvSummaryDm { id: string; name: string; }

export interface ConversationsControllerDeps {
  client: { list(token: string, wsId: string): Promise<{ group_id: string; type: string; visibility: string; name: string; member: boolean }[]> };
  create(args: { type: "dm" | "channel"; visibility?: "public" | "private"; name?: string; emailOrUsername?: string }): Promise<{ group_id: string; name: string; type: string }>;
  sendText(groupId: string, text: string): Promise<void> | void;
  // KT-verified MLS add. Adapter is wired in bootstrap.
  addMember(args: { wsId: string; group: string; identity: string; userId: string; currentMaxSeq: number; skipAddUser?: boolean }): Promise<void>;
  // External-commit join of a public channel the user isn't yet a member of.
  // Adapter is wired in bootstrap.
  joinPublic(args: { group: string; currentMaxSeq: number }): Promise<void>;
  // Seed a sync cursor so the server backfills this group's stored history.
  track(groupId: string, sinceSeq: number): void;
  conversation: {
    addMessage(groupID: string, m: { seq: number; sender: string; text: string }): void;
    // Highest seq currently held for the group (0 if none) — used to assign
    // collision-free optimistic-echo seqs.
    cursor(groupID: string): number;
  };
  token(): string;
  wsId(): string;
  userLabel(): string;
}

export function createConversationsController(deps: ConversationsControllerDeps) {
  const [channels, setChannels] = createSignal<ConvSummaryChannel[]>([]);
  const [dms, setDms] = createSignal<ConvSummaryDm[]>([]);
  const [activeId, setActiveId] = createSignal<string>("");
  // Provisional local seq for optimistic echoes (replaced by WS sync, UI-4). The
  // high base keeps echoes sorted after real (small-seq) server messages.
  const ECHO_BASE = 1_000_000_000;

  // Channels the user already belongs to (server-authoritative `member` flag from
  // `load`, plus any we join here). `joining` guards against a double external
  // commit if select fires twice before the first join resolves.
  const joined = new Set<string>();
  const joining = new Set<string>();

  // Join a public channel by external commit the first time the user opens one
  // they don't yet belong to — otherwise sending silently no-ops (the device
  // isn't in the channel's MLS group). DMs and private channels need an invite,
  // so they're skipped. Best-effort: a failure leaves the channel unjoined and
  // is logged; reselecting retries.
  async function ensureJoined(id: string): Promise<void> {
    const ch = channels().find((c) => c.id === id);
    if (!ch || ch.visibility !== "public" || joined.has(id) || joining.has(id)) return;
    joining.add(id);
    try {
      await deps.joinPublic({ group: id, currentMaxSeq: 0 });
      joined.add(id);
    } catch (e) {
      console.warn("joinPublic failed for", id, e);
    } finally {
      joining.delete(id);
    }
  }

  async function load() {
    // No active workspace yet → nothing to fetch. Without this guard the call
    // becomes `GET /workspaces//conversations` (empty id → 404, which `list`
    // throws on). That used to abort session-restore before the WebSocket
    // connected, leaving the app with no conversations AND offline.
    if (!deps.wsId()) {
      setChannels([]);
      setDms([]);
      return;
    }
    const all = await deps.client.list(deps.token(), deps.wsId());
    // Refresh membership from the server's authoritative `member` flag.
    joined.clear();
    for (const c of all) if (c.member) joined.add(c.group_id);
    setChannels(all.filter((c) => c.type === "channel").map((c) => ({ id: c.group_id, name: c.name, visibility: c.visibility })));
    setDms(all.filter((c) => c.type === "dm").map((c) => ({ id: c.group_id, name: c.name })));
    // Keep a conversation selected so the composer is never a silent no-op, but
    // re-pick whenever the current selection isn't in the freshly-loaded list —
    // e.g. after switching workspaces the previous workspace's active id is stale.
    // Prefer the first channel, else a DM.
    const valid = [...channels(), ...dms()].some((c) => c.id === activeId());
    if (!valid) setActiveId(channels()[0]?.id ?? dms()[0]?.id ?? "");
    for (const c of channels()) deps.track(c.id, 0);
    for (const m of dms()) deps.track(m.id, 0);
    // Make the auto-selected channel usable without an extra click.
    void ensureJoined(activeId());
  }

  return {
    channels, dms, activeId,
    load,
    // Selecting a public channel the user isn't in joins it (external commit) so
    // the composer works. The `member` flag from `load` tells us who needs it.
    async select(id: string) {
      setActiveId(id);
      await ensureJoined(id);
    },
    async createChannel(name: string, visibility: "public" | "private") {
      const conv = await deps.create({ type: "channel", visibility, name });
      await load();
      setActiveId(conv.group_id);
    },
    async startDm(person: { identity: string; userId: string }) {
      const conv = await deps.create({ type: "dm", emailOrUsername: person.identity });
      await load();
      setActiveId(conv.group_id);
      // The server CreateDM already makes both parties DM members, but only the
      // creator's devices are in the MLS group. Run the KT-verified device add
      // (skipAddUser, since DM membership is immutable) so the partner is Welcomed
      // and DM messages actually deliver.
      await deps.addMember({ wsId: deps.wsId(), group: conv.group_id, identity: person.identity, userId: person.userId, currentMaxSeq: 0, skipAddUser: true });
    },
    async send(text: string) {
      const id = activeId();
      if (!id) return;
      // Derive the echo seq from the group's current max so it stays unique and
      // monotonic across reloads (persisted echoes already occupy ECHO_BASE+n);
      // clamp to ECHO_BASE so echoes still sort above real server messages.
      const seq = Math.max(deps.conversation.cursor(id) + 1, ECHO_BASE);
      deps.conversation.addMessage(id, { seq, sender: deps.userLabel(), text });
      await deps.sendText(id, text);
    },
    // Add another workspace user to the active channel via the KT-verified MLS
    // add use-case. currentMaxSeq=0 is intentional: by MLS forward-secrecy the new
    // member can only decrypt messages from the epoch it joins onward, so it sees
    // messages sent AFTER the join — never the pre-join history.
    async addPeople(person: { identity: string; userId: string }) {
      const id = activeId();
      if (!id) return;
      await deps.addMember({ wsId: deps.wsId(), group: id, identity: person.identity, userId: person.userId, currentMaxSeq: 0 });
    },
  };
}
export type ConversationsController = ReturnType<typeof createConversationsController>;
