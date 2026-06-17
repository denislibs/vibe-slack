import { Show, type Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import { AuthPage } from "../pages/auth/AuthPage";
import { WorkspacePage } from "../pages/workspace/WorkspacePage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";
import type { SessionStore } from "../entities/session/store";
import type { WorkspaceStore } from "../entities/workspace/store";

export const App: Component<{
  groupId: string;
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
        messages={props.conversation.messages(props.groupId)}
        status={props.connection.status()}
        onSend={props.onSend}
      />
    </Show>
  </Show>
);
