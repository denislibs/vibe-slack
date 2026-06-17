import { For, Show, createMemo, type Component } from "solid-js";
import { Avatar } from "../../shared/ui";
import type { ChatMessage } from "../../entities/conversation/store";
import s from "./MessageList.module.css";

// A visual group of consecutive messages from the same sender.
type Group = { sender: string; messages: ChatMessage[] };

function groupBySender(messages: ChatMessage[]): Group[] {
  const groups: Group[] = [];
  for (const m of messages) {
    const last = groups[groups.length - 1];
    if (last && last.sender === m.sender) {
      last.messages.push(m);
    } else {
      groups.push({ sender: m.sender, messages: [m] });
    }
  }
  return groups;
}

export const MessageList: Component<{ messages: ChatMessage[]; title?: string }> = (props) => {
  const groups = createMemo(() => groupBySender(props.messages));
  return (
    <Show
      when={props.messages.length > 0}
      fallback={
        <div class={s.empty}>
          <div class={s.introTile}>#</div>
          <div class={s.introTitle}>
            This is the very beginning of #{props.title ?? "this conversation"}.
          </div>
          <div class={s.introSub}>Send a message to kick things off.</div>
        </div>
      }
    >
      <ul class={s.list} data-testid="message-list">
        <For each={groups()}>
          {(g) => (
            <li class={s.group}>
              <For each={g.messages}>
                {(m, i) => (
                  <Show
                    when={i() === 0}
                    fallback={
                      <div class={s.cont}>
                        <span class={s.text}>{m.text}</span>
                      </div>
                    }
                  >
                    <div class={s.lead}>
                      <Avatar name={m.sender} size={36} />
                      <div class={s.body}>
                        <div class={s.meta}>
                          <b class={s.sender}>{m.sender}</b>
                          <span class={s.time}>now</span>
                        </div>
                        <span class={s.text}>{m.text}</span>
                      </div>
                    </div>
                  </Show>
                )}
              </For>
            </li>
          )}
        </For>
      </ul>
    </Show>
  );
};
