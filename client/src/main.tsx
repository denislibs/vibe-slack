import "./styles/theme.css";
import { render } from "solid-js/web";
import { createSignal } from "solid-js";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";
import { CreateChannelModal } from "./widgets/create-channel-modal/CreateChannelModal";
import { NewDmModal } from "./widgets/new-dm-modal/NewDmModal";
import { createThemeStore } from "./entities/theme/store";

// Construct early so the default `data-theme` is applied to <html> before render.
// (UI-2 will wire the toggle into the UI; for now we just establish the attribute.)
const theme = createThemeStore();

const root = document.getElementById("root");
if (root) {
  const { conversation, connection, session, workspace, workspaces, wsClient, authFlow, conversations, setUserLabel, restoreSession } = bootstrap("device");
  const [authError, setAuthError] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [userEmail, setUserEmail] = createSignal("");
  const [createChannelOpen, setCreateChannelOpen] = createSignal(false);
  const [newDmOpen, setNewDmOpen] = createSignal(false);
  const [addPeopleOpen, setAddPeopleOpen] = createSignal(false);
  const [dmResults, setDmResults] = createSignal<{ user_id: string; username: string; email: string }[]>([]);
  const runQuery = async (q: string) => {
    const r = await wsClient.searchMembers(session.token() ?? "", workspace.current() ?? "", q);
    setDmResults(r);
  };
  const run = (fn: () => Promise<void>) => async () => {
    setBusy(true); setAuthError("");
    try { await fn(); } catch { setAuthError("invalid email or password"); }
    finally { setBusy(false); }
  };
  // Add the picked user to the active channel. A KT failure (UnverifiedDevice or
  // any KTError) fails closed — surface it and keep the modal context intact.
  const addPeople = async (person: { user_id: string; username: string; email: string }) => {
    setBusy(true); setAuthError("");
    try {
      await conversations.addPeople({ identity: person.username, userId: person.user_id });
      setAddPeopleOpen(false);
    } catch (e) {
      setAuthError(e instanceof Error && e.name === "UnverifiedDevice"
        ? "cannot add user: device not KT-verified"
        : "failed to add user");
    } finally { setBusy(false); }
  };
  // Restore-on-boot: probe the HttpOnly `session` cookie. On success the session
  // store flips to "onboarded" reactively → the App gate leaves the auth screen.
  // `userEmail`/`userLabel` aren't known after a refresh (no localStorage), so the
  // rail avatar shows "?" until the next login — acceptable for now.
  void restoreSession();

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
        onNewDm={() => setNewDmOpen(true)}
        onAddPeople={() => { setDmResults([]); setAddPeopleOpen(true); }}
      />
      <CreateChannelModal
        open={createChannelOpen()}
        onClose={() => setCreateChannelOpen(false)}
        onCreate={(name, vis) => { void conversations.createChannel(name, vis); setCreateChannelOpen(false); }}
      />
      <NewDmModal
        open={newDmOpen()}
        onClose={() => setNewDmOpen(false)}
        results={dmResults()}
        onQuery={(q) => void runQuery(q)}
        onPick={(u) => { void conversations.startDm(u.username); setNewDmOpen(false); }}
      />
      <NewDmModal
        open={addPeopleOpen()}
        onClose={() => setAddPeopleOpen(false)}
        results={dmResults()}
        onQuery={(q) => void runQuery(q)}
        title="Add people"
        onPick={(u) => void addPeople(u)}
      />
    </>
  ), root);
}
