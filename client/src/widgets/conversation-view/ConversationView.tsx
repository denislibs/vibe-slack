import { Show, type Component } from "solid-js";
import { MessageList } from "../message-list/MessageList";
import { Composer } from "../composer/Composer";
import { Button } from "../../shared/ui";
import type { ChatMessage } from "../../entities/conversation/store";
import s from "./ConversationView.module.css";

// Main conversation pane: header + scrollable message list + composer.
export const ConversationView: Component<{
  title: string;
  status: string;
  messages: ChatMessage[];
  onSend: (text: string) => void;
  // Shown only for channels (DMs have a fixed two-person membership).
  canAddPeople?: boolean;
  onAddPeople?: () => void;
}> = (props) => (
  <section class={s.view}>
    <header class={s.header}>
      <span class={s.title}># {props.title}</span>
      <span class={s.status} data-testid="status">{props.status}</span>
      <Show when={props.canAddPeople}>
        <span class={s.actions}>
          <Button variant="ghost" onClick={() => props.onAddPeople?.()}>Add people</Button>
        </span>
      </Show>
    </header>
    <div class={s.body}>
      <MessageList messages={props.messages} />
    </div>
    <footer class={s.footer}>
      <Composer onSend={props.onSend} />
    </footer>
  </section>
);
