import type { Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";

export const App: Component<{
  groupId: string;
  conversation: ConversationStore;
  connection: ConnectionStore;
  onSend: (text: string) => void;
}> = (props) => (
  <ChatPage
    messages={props.conversation.messages(props.groupId)}
    status={props.connection.status()}
    onSend={props.onSend}
  />
);
