// Resolve the DS WebSocket URL. Prefer an explicit override; otherwise same-origin /ws
// (dev: Vite proxies /ws to the backend; prod: same ingress). The old hardcoded
// ws://localhost:8080/ws was wrong (unproxied, wrong port) so the socket never connected.
export function resolveWsUrl(override: string | undefined | null, loc: { protocol: string; host: string }): string {
  if (override) return override;
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}/ws`;
}
