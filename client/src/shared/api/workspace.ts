type FetchFn = typeof fetch;

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  role: string;
}
export interface WorkspaceMember {
  user_id: string;
  username: string;
  email: string;
  role: string;
}

// Typed client for the workspace endpoints. All calls carry the device-bound
// session token as a Bearer header (same token the AS issues).
export class WorkspaceClient {
  // Default wraps fetch in an arrow so it's invoked unbound (calling native fetch as
  // a method, this.fetchFn(...), throws "Illegal invocation").
  constructor(private baseURL: string, private fetchFn: FetchFn = (...args) => fetch(...args)) {}

  private async call<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
    const init: RequestInit = {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    };
    if (body !== undefined) init.body = JSON.stringify(body);
    const res = await this.fetchFn(this.baseURL + path, init);
    if (!res.ok) throw new Error(`workspace ${method} ${path} failed: ${res.status}`);
    return (await res.json()) as T;
  }

  create(token: string, name: string): Promise<Workspace> {
    return this.call<Workspace>(token, "POST", "/workspaces", { name });
  }
  list(token: string): Promise<Workspace[]> {
    return this.call<Workspace[]>(token, "GET", "/workspaces");
  }
  members(token: string, workspaceId: string): Promise<WorkspaceMember[]> {
    return this.call<WorkspaceMember[]>(token, "GET", `/workspaces/${workspaceId}/members`);
  }
  addMember(token: string, workspaceId: string, emailOrUsername: string): Promise<WorkspaceMember> {
    return this.call<WorkspaceMember>(token, "POST", `/workspaces/${workspaceId}/members`, {
      email_or_username: emailOrUsername,
    });
  }
  searchMembers(token: string, wsId: string, q: string): Promise<WorkspaceMember[]> {
    return this.call<WorkspaceMember[]>(token, "GET", `/workspaces/${wsId}/members/search?q=${encodeURIComponent(q)}`);
  }
}
