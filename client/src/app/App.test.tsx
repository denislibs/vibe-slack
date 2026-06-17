// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";
import { createSessionStore } from "../entities/session/store";
import { createWorkspaceStore } from "../entities/workspace/store";

const selectedWorkspace = () => {
  const w = createWorkspaceStore();
  w.setList([{ id: "w1", name: "Acme", slug: "acme", role: "owner" }]);
  w.select("w1");
  return w;
};

describe("App", () => {
  it("renders the chat page wired to injected stores + send handler", () => {
    const conv = createConversationStore();
    conv.addMessage("g1", { seq: 1, sender: "alice", text: "wired" });
    const conn = createConnectionStore();
    conn.setStatus("online");
    const onSend = vi.fn();
    const session = createSessionStore();
    session.authenticated("t"); session.onboarded("d");

    const { getByText, getByTestId } = render(() => (
      <App conversation={conv} connection={conn} session={session} workspace={selectedWorkspace()} onLogin={() => {}} onRegister={() => {}} onCreateWorkspace={() => {}} onSelectWorkspace={() => {}} authError="" busy={false} onSend={onSend} theme="dark" onToggleTheme={() => {}} userEmail="" channels={[]} dms={[]} activeId="g1" onSelect={() => {}} />
    ));
    expect(getByText("wired")).toBeTruthy();
    expect(getByTestId("status").textContent).toBe("online");
  });

  it("re-renders when the conversation store gains a message after mount", async () => {
    const conv = createConversationStore();
    const conn = createConnectionStore();
    conn.setStatus("online");
    const session = createSessionStore();
    session.authenticated("t"); session.onboarded("d");
    const { queryByText, findByText } = render(() => (
      <App conversation={conv} connection={conn} session={session} workspace={selectedWorkspace()} onLogin={() => {}} onRegister={() => {}} onCreateWorkspace={() => {}} onSelectWorkspace={() => {}} authError="" busy={false} onSend={() => {}} theme="dark" onToggleTheme={() => {}} userEmail="" channels={[]} dms={[]} activeId="g1" onSelect={() => {}} />
    ));
    expect(queryByText("late message")).toBeNull();
    // Mutate the store AFTER mount — the DOM must update reactively.
    conv.addMessage("g1", { seq: 1, sender: "bob", text: "late message" });
    expect(await findByText("late message")).toBeTruthy();
  });

  it("re-renders when connection status changes after mount", async () => {
    const conv = createConversationStore();
    const conn = createConnectionStore();
    const session = createSessionStore();
    session.authenticated("t"); session.onboarded("d");
    const { getByTestId } = render(() => (
      <App conversation={conv} connection={conn} session={session} workspace={selectedWorkspace()} onLogin={() => {}} onRegister={() => {}} onCreateWorkspace={() => {}} onSelectWorkspace={() => {}} authError="" busy={false} onSend={() => {}} theme="dark" onToggleTheme={() => {}} userEmail="" channels={[]} dms={[]} activeId="g1" onSelect={() => {}} />
    ));
    expect(getByTestId("status").textContent).toBe("offline");
    conn.setStatus("online");
    // findBy/await a microtask for Solid to flush.
    await Promise.resolve();
    expect(getByTestId("status").textContent).toBe("online");
  });
});

describe("App auth gate", () => {
  const base = (session: ReturnType<typeof createSessionStore>) => ({
    conversation: createConversationStore(), connection: createConnectionStore(),
    session, workspace: selectedWorkspace(),
    onLogin: vi.fn(), onRegister: vi.fn(), onCreateWorkspace: vi.fn(), onSelectWorkspace: vi.fn(),
    onSend: vi.fn(), authError: "", busy: false, theme: "dark" as const, onToggleTheme: vi.fn(), userEmail: "",
    channels: [], dms: [], activeId: "g1", onSelect: vi.fn(),
  });

  it("shows the auth page when anonymous", () => {
    const { getByText } = render(() => <App {...base(createSessionStore())} />);
    expect(getByText("Sign in")).toBeTruthy();
  });

  it("shows the chat when onboarded", () => {
    const session = createSessionStore();
    session.authenticated("TOK"); session.onboarded("DEV1");
    const props = base(session);
    props.conversation.addMessage("g1", { seq: 1, sender: "alice", text: "hello-chat" });
    props.connection.setStatus("online");
    const { getByText } = render(() => <App {...props} />);
    expect(getByText("hello-chat")).toBeTruthy();
  });
});

describe("App workspace gate", () => {
  it("shows the workspace page when onboarded with no workspace selected", () => {
    const session = createSessionStore();
    session.authenticated("TOK"); session.onboarded("DEV1");
    const workspace = createWorkspaceStore();
    workspace.setList([{ id: "w1", name: "Acme", slug: "acme", role: "owner" }]);
    const { getByText } = render(() => (
      <App
        conversation={createConversationStore()} connection={createConnectionStore()}
        session={session} workspace={workspace}
        onLogin={vi.fn()} onRegister={vi.fn()} onCreateWorkspace={vi.fn()} onSelectWorkspace={vi.fn()}
        onSend={vi.fn()} authError="" busy={false} theme="dark" onToggleTheme={() => {}} userEmail=""
        channels={[]} dms={[]} activeId="g1" onSelect={vi.fn()}
      />
    ));
    expect(getByText("Workspaces")).toBeTruthy();
  });

  it("shows the chat once a workspace is selected", () => {
    const session = createSessionStore();
    session.authenticated("TOK"); session.onboarded("DEV1");
    const workspace = createWorkspaceStore();
    workspace.setList([{ id: "w1", name: "Acme", slug: "acme", role: "owner" }]);
    const conversation = createConversationStore();
    conversation.addMessage("g1", { seq: 1, sender: "alice", text: "ws-chat" });
    const connection = createConnectionStore();
    connection.setStatus("online");
    workspace.select("w1");
    const { getByText } = render(() => (
      <App
        conversation={conversation} connection={connection}
        session={session} workspace={workspace}
        onLogin={vi.fn()} onRegister={vi.fn()} onCreateWorkspace={vi.fn()} onSelectWorkspace={vi.fn()}
        onSend={vi.fn()} authError="" busy={false} theme="dark" onToggleTheme={() => {}} userEmail=""
        channels={[]} dms={[]} activeId="g1" onSelect={vi.fn()}
      />
    ));
    expect(getByText("ws-chat")).toBeTruthy();
  });
});
