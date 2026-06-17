import { createSignal } from "solid-js";

export type SessionStatus = "anonymous" | "authenticated" | "onboarded";

// Session token kept in memory (not localStorage) to limit XSS token theft.
export function createSessionStore() {
  const [status, setStatus] = createSignal<SessionStatus>("anonymous");
  const [token, setToken] = createSignal("");
  const [deviceId, setDeviceId] = createSignal("");
  return {
    status, token, deviceId,
    authenticated(t: string) { setToken(t); setStatus("authenticated"); },
    onboarded(dev: string) { setDeviceId(dev); setStatus("onboarded"); },
    clear() { setToken(""); setDeviceId(""); setStatus("anonymous"); },
  };
}

export type SessionStore = ReturnType<typeof createSessionStore>;
