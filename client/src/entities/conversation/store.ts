import { createStore, unwrap } from "solid-js/store";

export type ChatMessage = { seq: number; sender: string; text: string };

type State = { byGroup: Record<string, ChatMessage[]> };

// Optional persistence adapter. `save` receives a plain snapshot of the whole
// byGroup map on every mutating addMessage; `load` returns previously persisted
// data (or null) for hydration on boot.
export interface ConversationPersist {
  load(): Promise<Record<string, ChatMessage[]> | null>;
  save(byGroup: Record<string, ChatMessage[]>): void;
}

// Conversation store: decrypted messages per group, ordered by seq, deduped.
export function createConversationStore(persist?: ConversationPersist) {
  const [state, setState] = createStore<State>({ byGroup: {} });

  function addMessage(groupID: string, msg: ChatMessage) {
    const cur = state.byGroup[groupID] ?? [];
    if (cur.some((m) => m.seq === msg.seq)) return; // dedupe
    const next = [...cur, msg].sort((a, b) => a.seq - b.seq);
    setState("byGroup", groupID, next);
    // Persist a plain snapshot — unwrap the Solid store proxy and deep-clone via
    // JSON (data is plain, portable across worker/test environments).
    persist?.save(JSON.parse(JSON.stringify(unwrap(state.byGroup))) as Record<string, ChatMessage[]>);
  }

  return {
    messages(groupID: string): ChatMessage[] {
      return state.byGroup[groupID] ?? [];
    },
    cursor(groupID: string): number {
      const m = state.byGroup[groupID] ?? [];
      return m.length ? m[m.length - 1].seq : 0;
    },
    addMessage,
    async hydrate(): Promise<void> {
      if (!persist) return;
      const saved = await persist.load();
      if (!saved) return;
      for (const g of Object.keys(saved)) for (const m of saved[g]) addMessage(g, m);
    },
  };
}

export type ConversationStore = ReturnType<typeof createConversationStore>;
