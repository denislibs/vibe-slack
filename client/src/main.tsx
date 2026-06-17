import "./styles/theme.css";
import { render } from "solid-js/web";
import { createSignal } from "solid-js";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";
import { CreateChannelModal } from "./widgets/create-channel-modal/CreateChannelModal";
import { createThemeStore } from "./entities/theme/store";

// Construct early so the default `data-theme` is applied to <html> before render.
// (UI-2 will wire the toggle into the UI; for now we just establish the attribute.)
const theme = createThemeStore();

const root = document.getElementById("root");
if (root) {
  const { conversation, connection, session, workspace, workspaces, authFlow, conversations, setUserLabel } = bootstrap("device");
  const [authError, setAuthError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [userEmail, setUserEmail] = createSignal("");
  const [createChannelOpen, setCreateChannelOpen] = createSignal(false);
  const run = (fn: () => Promise<void>) => async () => {
    setBusy(true); setAuthError("");
    try { await fn(); } catch { setAuthError("invalid email or password"); }
    finally { setBusy(false); }
  };
  render(() => (
    <>
      <App
        conversation={conversation}
        connection={connection}
        session={session}
        workspace={workspace}
        onLogin={(email, pw) => void run(async () => {
          await authFlow.login(email, pw);
          setUserEmail(email);
          setUserLabel(email);
          await workspaces.load();
          await conversations.load();
        })()}
        onRegister={(email, username, pw) => void run(() => authFlow.register(email, username, pw))()}
        onCreateWorkspace={(name) => void run(() => workspaces.create(name))()}
        onSelectWorkspace={(id) => workspace.select(id)}
        onSend={(text) => void conversations.send(text)}
        authError={authError()}
        busy={busy()}
        theme={theme.theme()}
        onToggleTheme={() => theme.toggle()}
        userEmail={userEmail()}
        channels={conversations.channels()}
        dms={conversations.dms()}
        activeId={conversations.activeId()}
        onSelect={(id) => conversations.select(id)}
        onCreateChannel={() => setCreateChannelOpen(true)}
        onNewDm={() => {/* UI3-3 */}}
      />
      <CreateChannelModal
        open={createChannelOpen()}
        onClose={() => setCreateChannelOpen(false)}
        onCreate={(name, vis) => { void conversations.createChannel(name, vis); setCreateChannelOpen(false); }}
      />
    </>
  ), root);
}
