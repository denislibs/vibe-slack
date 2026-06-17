import { createSignal } from "solid-js";

export interface ConvSummaryChannel { id: string; name: string; visibility: string; }
export interface ConvSummaryDm { id: string; name: string; }

export interface ConversationsControllerDeps {
  client: { list(token: string, wsId: string): Promise<{ group_id: string; type: string; visibility: string; name: string }[]> };
  create(args: { type: "dm" | "channel"; visibility?: "public" | "private"; name?: string; emailOrUsername?: string }): Promise<{ group_id: string; name: string; type: string }>;
  sendText(groupId: string, text: string): Promise<void> | void;
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
  };
}
export type ConversationsController = ReturnType<typeof createConversationsController>;
