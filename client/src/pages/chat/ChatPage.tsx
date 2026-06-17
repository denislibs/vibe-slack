import type { Component } from "solid-js";
import { MessageList } from "../../widgets/message-list/MessageList";
import { Composer } from "../../widgets/composer/Composer";
import type { ChatMessage } from "../../entities/conversation/store";

// Presentational page: data + callbacks via props. No business logic, no direct
// worker/store access (clean architecture).
export const ChatPage: Component<{
  messages: ChatMessage[];
  status: string;
  onSend: (text: string) => void;
}> = (props) => (
  <div>
    <header data-testid="status">{props.status}</header>
    <MessageList messages={props.messages} />
    <Composer onSend={props.onSend} />
  </div>
);
