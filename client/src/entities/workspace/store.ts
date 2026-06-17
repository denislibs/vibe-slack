import { createSignal } from "solid-js";
import type { Workspace } from "../../shared/api/workspace";

// Current-workspace context: list of my workspaces + the active selection.
// Kept in memory like the session token (no persistence in this milestone).
export function createWorkspaceStore() {
  const [list, setList] = createSignal<Workspace[]>([]);
  const [current, setCurrent] = createSignal<string | null>(null);
  return {
    list,
    current,
    setList: (ws: Workspace[]) => setList(ws),
    select: (id: string) => setCurrent(id),
    clear: () => {
      setList([]);
      setCurrent(null);
    },
  };
}
export type WorkspaceStore = ReturnType<typeof createWorkspaceStore>;
