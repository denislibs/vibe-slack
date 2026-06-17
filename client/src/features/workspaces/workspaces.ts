import type { WorkspaceStore } from "../../entities/workspace/store";
import type { Workspace } from "../../shared/api/workspace";

export interface WorkspacesDeps {
  client: {
    create(token: string, name: string): Promise<Workspace>;
    list(token: string): Promise<Workspace[]>;
  };
  store: WorkspaceStore;
  token: () => string;
}

// Use-cases for the workspace gate. Auto-selects when the user has exactly one
// workspace so the common case skips the picker.
export function createWorkspaces(deps: WorkspacesDeps) {
  return {
    async load(): Promise<void> {
      const ws = await deps.client.list(deps.token());
      deps.store.setList(ws);
      if (ws.length === 1) deps.store.select(ws[0].id);
    },
    async create(name: string): Promise<void> {
      const ws = await deps.client.create(deps.token(), name);
      deps.store.setList([...deps.store.list(), ws]);
      deps.store.select(ws.id);
    },
  };
}
