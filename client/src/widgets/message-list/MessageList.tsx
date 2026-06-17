import { For, type Component } from "solid-js";
import type { ChatMessage } from "../../entities/conversation/store";

export const MessageList: Component<{ messages: ChatMessage[] }> = (props) => (
  <ul data-testid="message-list">
    <For each={props.messages}>
      {(m) => <li><b>{m.sender}:</b> {m.text}</li>}
    </For>
  </ul>
);
