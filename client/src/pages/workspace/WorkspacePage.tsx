import { For, createSignal, type Component } from "solid-js";
import type { Workspace } from "../../shared/api/workspace";

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
    <div>
      <h1>Workspaces</h1>
      <ul>
        <For each={props.workspaces}>
          {(w) => (
            <li>
              <button onClick={() => props.onSelect(w.id)}>{w.name}</button>
            </li>
          )}
        </For>
      </ul>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name().trim()) props.onCreate(name().trim());
        }}
      >
        <input aria-label="Workspace name" value={name()} onInput={(e) => setName(e.currentTarget.value)} />
        <button type="submit" disabled={props.busy}>Create</button>
      </form>
    </div>
  );
};
