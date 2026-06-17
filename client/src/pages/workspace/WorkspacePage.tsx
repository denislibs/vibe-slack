import { For, createSignal, type Component } from "solid-js";
import type { Workspace } from "../../shared/api/workspace";
import { Avatar, Button, Input } from "../../shared/ui";
import s from "./WorkspacePage.module.css";

// Workspace gate: pick an existing workspace or create a new one. Presentational
// only — selection/creation are delegated to callbacks.
export const WorkspacePage: Component<{
  workspaces: Workspace[];
  onSelect: (id: string) => void;
  onCreate: (name: string) => void;
  busy: boolean;
}> = (props) => {
  const [name, setName] = createSignal("");
  return (
    <div class={s.page}>
      <div class={s.card}>
        <h1 class={s.title}>Workspaces</h1>
        <ul class={s.list}>
          <For each={props.workspaces}>
            {(w) => (
              <li>
                <button class={s.row} onClick={() => props.onSelect(w.id)}>
                  <Avatar name={w.name} size={28} />
                  {w.name}
                </button>
              </li>
            )}
          </For>
        </ul>
        <form
          class={s.create}
          onSubmit={(e) => {
            e.preventDefault();
            if (name().trim()) props.onCreate(name().trim());
          }}
        >
          <Input aria-label="Workspace name" value={name()} onInput={(e) => setName(e.currentTarget.value)} />
          <Button type="submit" variant="primary" disabled={props.busy}>Create</Button>
        </form>
      </div>
    </div>
  );
};
