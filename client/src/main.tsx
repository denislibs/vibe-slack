import { render } from "solid-js/web";
import { createSignal } from "solid-js";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";

const root = document.getElementById("root");
if (root) {
  const { orchestrator, conversation, connection, session, authFlow } = bootstrap("device");
  const [authError, setAuthError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const run = (fn: () => Promise<void>) => async () => {
    setBusy(true); setAuthError("");
    try { await fn(); } catch { setAuthError("invalid email or password"); }
    finally { setBusy(false); }
  };
  render(() => (
    <App
      groupId="g1"
      conversation={conversation}
      connection={connection}
      session={session}
      onLogin={(email, pw) => void run(() => authFlow.login(email, pw))()}
      onRegister={(email, username, pw) => void run(() => authFlow.register(email, username, pw))()}
      onSend={(text) => orchestrator.sendText("g1", text)}
      authError={authError()}
      busy={busy()}
    />
  ), root);
}
