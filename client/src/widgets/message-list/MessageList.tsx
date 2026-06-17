import { For, Show, type Component } from "solid-js";
import { Avatar } from "../../shared/ui";
import type { ChatMessage } from "../../entities/conversation/store";
import s from "./MessageList.module.css";

export const MessageList: Component<{ messages: ChatMessage[] }> = (props) => (
  <Show
    when={props.messages.length > 0}
    fallback={<div class={s.empty}>No messages yet</div>}
  >
    <ul class={s.list} data-testid="message-list">
      <For each={props.messages}>
        {(m) => (
          <li class={s.row}>
            <Avatar name={m.sender} size={36} />
            <div class={s.body}>
              <b class={s.sender}>{m.sender}</b>
              <span class={s.text}>{m.text}</span>
            </div>
          </li>
        )}
      </For>
    </ul>
  </Show>
);
