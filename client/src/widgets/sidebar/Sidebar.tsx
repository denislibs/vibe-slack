import { For, Show, type Component } from "solid-js";
import { Avatar } from "../../shared/ui";
import s from "./Sidebar.module.css";

type Channel = { id: string; name: string; visibility: string };
type Dm = { id: string; name: string };

// Channel + DM navigation list for the active workspace.
export const Sidebar: Component<{
  workspaceName: string;
  channels: Channel[];
  dms: Dm[];
  activeId: string;
  onSelect: (id: string) => void;
  onAddChannel?: () => void;
  onNewDm?: () => void;
}> = (props) => {
  const rowClass = (id: string) => `${s.row} ${id === props.activeId ? s.active : ""}`;
  return (
    <aside class={s.sidebar}>
      <div class={s.header}>{props.workspaceName}</div>

      <div class={s.section}>
        <div class={s.label}>Channels</div>
        <Show
          when={props.channels.length > 0}
          fallback={<div class={s.muted}>No channels yet</div>}
        >
          <For each={props.channels}>
            {(c) => (
              <div class={rowClass(c.id)} onClick={() => props.onSelect(c.id)}>
                {c.visibility === "private" ? "🔒" : "#"} {c.name}
              </div>
            )}
          </For>
        </Show>
        <div class={`${s.row} ${s.muted}`} onClick={() => props.onAddChannel?.()}>
          + Add channels
        </div>
      </div>

      <div class={s.section}>
        <div class={s.label}>Direct messages</div>
        <For each={props.dms}>
          {(d) => (
            <div class={`${rowClass(d.id)} ${s.dmRow}`} onClick={() => props.onSelect(d.id)}>
              <Avatar name={d.name} size={20} /> {d.name}
            </div>
          )}
        </For>
        <div class={`${s.row} ${s.muted}`} onClick={() => props.onNewDm?.()}>
          + New message
        </div>
      </div>
    </aside>
  );
};
