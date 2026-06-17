import { createStore } from "solid-js/store";

export type ChatMessage = { seq: number; sender: string; text: string };

type State = { byGroup: Record<string, ChatMessage[]> };

// Conversation store: decrypted messages per group, ordered by seq, deduped.
export function createConversationStore() {
  const [state, setState] = createStore<State>({ byGroup: {} });

  return {
    messages(groupID: string): ChatMessage[] {
      return state.byGroup[groupID] ?? [];
    },
    cursor(groupID: string): number {
      const m = state.byGroup[groupID] ?? [];
      return m.length ? m[m.length - 1].seq : 0;
    },
    addMessage(groupID: string, msg: ChatMessage) {
      const cur = state.byGroup[groupID] ?? [];
      if (cur.some((m) => m.seq === msg.seq)) return; // dedupe
      const next = [...cur, msg].sort((a, b) => a.seq - b.seq);
      setState("byGroup", groupID, next);
    },
  };
}

export type ConversationStore = ReturnType<typeof createConversationStore>;
