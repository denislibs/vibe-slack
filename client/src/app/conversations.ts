import { createSignal } from "solid-js";

export interface ConvSummaryChannel { id: string; name: string; visibility: string; }
export interface ConvSummaryDm { id: string; name: string; }

export interface ConversationsControllerDeps {
  client: { list(token: string, wsId: string): Promise<{ group_id: string; type: string; visibility: string; name: string }[]> };
  create(args: { type: "dm" | "channel"; visibility?: "public" | "private"; name?: string; emailOrUsername?: string }): Promise<{ group_id: string; name: string; type: string }>;
  sendText(groupId: string, text: string): Promise<void> | void;
  // KT-verified MLS add. Adapter is wired in bootstrap.
  addMember(args: { wsId: string; group: string; identity: string; userId: string; currentMaxSeq: number }): Promise<void>;
  // Seed a sync cursor so the server backfills this group's stored history.
  track(groupId: string, sinceSeq: number): void;
  conversation: { addMessage(groupID: string, m: { seq: number; sender: string; text: string }): void };
  token(): string;
  wsId(): string;
  userLabel(): string;
}

export function createConversationsController(deps: ConversationsControllerDeps) {
  const [channels, setChannels] = createSignal<ConvSummaryChannel[]>([]);
  const [dms, setDms] = createSignal<ConvSummaryDm[]>([]);
  const [activeId, setActiveId] = createSignal<string>("");
  let echoSeq = 1_000_000_000; // provisional local seq; replaced by WS sync (UI-4)

  async function load() {
    const all = await deps.client.list(deps.token(), deps.wsId());
    setChannels(all.filter((c) => c.type === "channel").map((c) => ({ id: c.group_id, name: c.name, visibility: c.visibility })));
    setDms(all.filter((c) => c.type === "dm").map((c) => ({ id: c.group_id, name: c.name })));
    // Auto-select a conversation so the composer is never a silent no-op (sending
    // with no active conversation does nothing). Prefer the first channel, else a DM.
    if (!activeId()) {
      const first = channels()[0]?.id ?? dms()[0]?.id;
      if (first) setActiveId(first);
    }
    for (const c of channels()) deps.track(c.id, 0);
    for (const m of dms()) deps.track(m.id, 0);
  }

  return {
    channels, dms, activeId,
    load,
    // TODO(UI-4): join public on select. The `list` payload has no membership
    // flag, so we cannot tell whether the user already belongs to a public
    // channel; auto-joining safely needs membership tracking the list does not
    // provide. Deferred per task scope — for now select shows channels the user
    // is already a member of.
    select: (id: string) => setActiveId(id),
    async createChannel(name: string, visibility: "public" | "private") {
      const conv = await deps.create({ type: "channel", visibility, name });
      await load();
      setActiveId(conv.group_id);
    },
    async startDm(emailOrUsername: string) {
      const conv = await deps.create({ type: "dm", emailOrUsername });
      await load();
      setActiveId(conv.group_id);
    },
    async send(text: string) {
      const id = activeId();
      if (!id) return;
      deps.conversation.addMessage(id, { seq: echoSeq++, sender: deps.userLabel(), text });
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
