// Postable is the minimal worker surface the RPC needs (real Worker satisfies it).
export interface Postable {
  postMessage(message: unknown): void;
  onmessage: ((ev: MessageEvent) => void) | null;
}

type Pending = { resolve: (v: unknown) => void; reject: (e: Error) => void };

type Response =
  | { id: string; ok: true; result: unknown }
  | { id: string; ok: false; error: string };

function isResponse(v: unknown): v is Response {
  if (typeof v !== "object" || v === null) return false;
  const o = v as Record<string, unknown>;
  return typeof o.id === "string" && typeof o.ok === "boolean";
}

export interface WorkerRpc {
  call(kind: string, payload: unknown): Promise<unknown>;
  /** Subscribe to fire-and-forget events the worker pushes (no id). */
  onEvent(handler: (event: string, payload: unknown) => void): void;
}

export function createWorkerRpc(worker: Postable): WorkerRpc {
  let seq = 0;
  const pending = new Map<string, Pending>();
  let eventHandler: ((event: string, payload: unknown) => void) | null = null;

  worker.onmessage = (ev: MessageEvent) => {
    const data = ev.data;
    if (isResponse(data)) {
      const p = pending.get(data.id);
      if (!p) return;
      pending.delete(data.id);
      if (data.ok) p.resolve(data.result);
      else p.reject(new Error(data.error));
      return;
    }
    if (typeof data === "object" && data !== null && "event" in (data as object)) {
      const e = data as { event: string; payload: unknown };
      eventHandler?.(e.event, e.payload);
    }
  };

  return {
    call(kind, payload) {
      const id = `r${++seq}`;
      return new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
        worker.postMessage({ id, kind, payload });
      });
    },
    onEvent(handler) {
      eventHandler = handler;
    },
  };
}
