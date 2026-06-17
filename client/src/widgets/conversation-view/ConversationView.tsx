import type { Component } from "solid-js";
import { MessageList } from "../message-list/MessageList";
import { Composer } from "../composer/Composer";
import type { ChatMessage } from "../../entities/conversation/store";
import s from "./ConversationView.module.css";

// Main conversation pane: header + scrollable message list + composer.
export const ConversationView: Component<{
  title: string;
  status: string;
  messages: ChatMessage[];
  onSend: (text: string) => void;
}> = (props) => (
  <section class={s.view}>
    <header class={s.header}>
      <span class={s.title}># {props.title}</span>
      <span class={s.status} data-testid="status">{props.status}</span>
    </header>
    <div class={s.body}>
      <MessageList messages={props.messages} />
    </div>
    <footer class={s.footer}>
      <Composer onSend={props.onSend} />
    </footer>
  </section>
);
