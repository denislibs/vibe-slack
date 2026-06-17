import { createSignal } from "solid-js";

export type ConnStatus = "offline" | "connecting" | "online";

export function createConnectionStore() {
  const [status, setStatus] = createSignal<ConnStatus>("offline");
  return { status, setStatus };
}

export type ConnectionStore = ReturnType<typeof createConnectionStore>;
