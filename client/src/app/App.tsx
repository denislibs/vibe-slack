import { Show, type Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import { AuthPage } from "../pages/auth/AuthPage";
import { WorkspacePage } from "../pages/workspace/WorkspacePage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";
import type { SessionStore } from "../entities/session/store";
import type { WorkspaceStore } from "../entities/workspace/store";

type Channel = { id: string; name: string; visibility: string };
type Dm = { id: string; name: string };

export const App: Component<{
  conversation: ConversationStore;
  connection: ConnectionStore;
  session: SessionStore;
  workspace: WorkspaceStore;
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  onCreateWorkspace: (name: string) => void;
  onSelectWorkspace: (id: string) => void;
  onSend: (text: string) => void;
  authError: string;
  busy: boolean;
  theme: "dark" | "light";
  onToggleTheme: () => void;
  userEmail: string;
  channels: Channel[];
  dms: Dm[];
  activeId: string;
  onSelect: (id: string) => void;
  onCreateChannel?: () => void;
  onNewDm?: () => void;
  onAddPeople?: () => void;
}> = (props) => (
  <Show
    when={props.session.status() !== "anonymous"}
    fallback={<AuthPage onLogin={props.onLogin} onRegister={props.onRegister} error={props.authError} busy={props.busy} />}
  >
    <Show
      when={props.workspace.current() !== null}
      fallback={
        <WorkspacePage
          workspaces={props.workspace.list()}
          onSelect={props.onSelectWorkspace}
          onCreate={props.onCreateWorkspace}
          busy={props.busy}
        />
      }
    >
      <ChatPage
        messages={props.conversation.messages(props.activeId)}
        status={props.connection.status()}
        onSend={props.onSend}
        workspaceName={
          props.workspace.list().find((w) => w.id === props.workspace.current())?.name ??
          "Workspace"
        }
        userEmail={props.userEmail}
        theme={props.theme}
        onToggleTheme={props.onToggleTheme}
        channels={props.channels}
        dms={props.dms}
        activeId={props.activeId}
        onSelect={props.onSelect}
        onAddChannel={props.onCreateChannel}
        onNewDm={props.onNewDm}
        onAddPeople={props.onAddPeople}
      />
    </Show>
  </Show>
);
