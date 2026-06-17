import type { Component } from "solid-js";
import { WorkspaceRail } from "../../widgets/workspace-rail/WorkspaceRail";
import { Topbar } from "../../widgets/topbar/Topbar";
import { Sidebar } from "../../widgets/sidebar/Sidebar";
import { ConversationView } from "../../widgets/conversation-view/ConversationView";
import type { ChatMessage } from "../../entities/conversation/store";
import s from "./ChatPage.module.css";

type Channel = { id: string; name: string; visibility: string };
type Dm = { id: string; name: string };

// Presentational shell: 3-column Slack layout. Data + callbacks via props.
export const ChatPage: Component<{
  messages: ChatMessage[];
  status: string;
  onSend: (text: string) => void;
  workspaceName: string;
  userEmail: string;
  theme: "dark" | "light";
  onToggleTheme: () => void;
  channels: Channel[];
  dms: Dm[];
  activeId: string;
  onSelect: (id: string) => void;
  onAddChannel?: () => void;
  onNewDm?: () => void;
  onAddPeople?: () => void;
}> = (props) => {
  const title = () =>
    props.channels.find((c) => c.id === props.activeId)?.name ??
    props.dms.find((d) => d.id === props.activeId)?.name ??
    "Messenger";
  // Only channels can have people added; DMs are a fixed two-person membership.
  const isChannel = () => props.channels.some((c) => c.id === props.activeId);
  return (
    <div class={s.shell}>
      <Topbar workspaceName={props.workspaceName} />
      <div class={s.columns}>
        <WorkspaceRail
          workspaceName={props.workspaceName}
          userEmail={props.userEmail}
          theme={props.theme}
          onToggleTheme={props.onToggleTheme}
        />
        <Sidebar
          workspaceName={props.workspaceName}
          channels={props.channels}
          dms={props.dms}
          activeId={props.activeId}
          onSelect={props.onSelect}
          onAddChannel={props.onAddChannel}
          onNewDm={props.onNewDm}
        />
        <ConversationView
          title={title()}
          status={props.status}
          messages={props.messages}
          onSend={props.onSend}
          canAddPeople={isChannel()}
          onAddPeople={props.onAddPeople}
        />
      </div>
    </div>
  );
};
