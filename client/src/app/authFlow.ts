import type { SessionStore } from "../entities/session/store";

export interface AuthFlowDeps {
  authenticator: {
    register(email: string, password: string): Promise<void>;
    login(email: string, password: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }>;
  };
  onboard(token: string): Promise<string>;   // registers this device, returns its id
  connect(token: string): void;              // dials the DS WebSocket with the device-bound token
  session: SessionStore;
}

// Orchestrates sign-in: authenticate, onboard a device if required, then connect.
// Business logic in the app layer; UI invokes it via callbacks.
export function createAuthFlow(deps: AuthFlowDeps) {
  return {
    register: (email: string, password: string) => deps.authenticator.register(email, password),
    async login(email: string, password: string): Promise<void> {
      const { sessionToken, deviceEnrollRequired } = await deps.authenticator.login(email, password);
      deps.session.authenticated(sessionToken);
      if (deviceEnrollRequired) {
        deps.session.onboarded(await deps.onboard(sessionToken));
      }
      deps.connect(sessionToken);
    },
  };
}
