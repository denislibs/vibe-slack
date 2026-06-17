// The OPAQUE server identity is fixed by the AS contract.
const SERVER_ID_B64 = btoa("messenger-as");
// Passwords are base64-encoded for the wasm boundary (it expects base64 bytes).
const pwB64 = (pw: string) => btoa(unescape(encodeURIComponent(pw)));

export interface OpaqueOpsLike {
  regInit(pwB64: string): { flowId: string; request: string };
  regFinalize(flowId: string, respB64: string, serverIdB64: string): { record: string; exportKey: string };
  loginKE1(pwB64: string): { flowId: string; ke1: string };
  loginKE3(flowId: string, ke2B64: string, serverIdB64: string): { ke3: string; sessionKey: string };
}
export interface AsLike {
  registerStart(email: string, request: string): Promise<string>;
  registerFinish(email: string, username: string, record: string): Promise<void>;
  loginStart(email: string, ke1: string): Promise<{ loginId: string; ke2: string }>;
  loginFinish(loginId: string, ke3: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }>;
}

export function createAuthenticator(opaque: OpaqueOpsLike, as: AsLike) {
  return {
    async register(email: string, username: string, password: string): Promise<void> {
      const init = opaque.regInit(pwB64(password));
      const response = await as.registerStart(email, init.request);
      const fin = opaque.regFinalize(init.flowId, response, SERVER_ID_B64);
      await as.registerFinish(email, username, fin.record);
    },
    async login(email: string, password: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }> {
      const ke1 = opaque.loginKE1(pwB64(password));
      const start = await as.loginStart(email, ke1.ke1);
      const ke3 = opaque.loginKE3(ke1.flowId, start.ke2, SERVER_ID_B64);
      return as.loginFinish(start.loginId, ke3.ke3);
    },
  };
}
