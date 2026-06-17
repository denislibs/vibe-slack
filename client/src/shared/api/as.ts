type FetchFn = typeof fetch;

// Thin typed wrapper over the Authentication Service HTTP API. All OPAQUE byte fields
// are base64 strings (matches the server contract).
export class AsClient {
  constructor(private baseURL: string, private fetchFn: FetchFn = fetch) {}

  private async post<T>(path: string, body: unknown, token?: string): Promise<T> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await this.fetchFn(this.baseURL + path, { method: "POST", headers, body: JSON.stringify(body) });
    if (!res.ok) {
      let code = "error";
      try { code = ((await res.json()) as { error?: string }).error ?? code; } catch { /* ignore */ }
      throw new Error(`AS ${path} failed: ${res.status} ${code}`);
    }
    return (await res.json()) as T;
  }

  async registerStart(email: string, opaqueRegistrationRequest: string): Promise<string> {
    const r = await this.post<{ opaque_registration_response: string }>(
      "/auth/register/start", { email, opaque_registration_request: opaqueRegistrationRequest });
    return r.opaque_registration_response;
  }
  async registerFinish(email: string, record: string): Promise<void> {
    await this.post("/auth/register/finish", { email, opaque_registration_record: record });
  }
  async loginStart(email: string, ke1: string): Promise<{ loginId: string; ke2: string }> {
    const r = await this.post<{ login_id: string; ke2: string }>("/auth/login/start", { email, ke1 });
    return { loginId: r.login_id, ke2: r.ke2 };
  }
  async loginFinish(loginId: string, ke3: string): Promise<{ sessionToken: string; deviceEnrollRequired: boolean }> {
    const r = await this.post<{ session_token: string; device_enroll_required: boolean }>(
      "/auth/login/finish", { login_id: loginId, ke3 });
    return { sessionToken: r.session_token, deviceEnrollRequired: r.device_enroll_required };
  }
  async enrollDevice(token: string, signingPublicKey: string, label: string, initialKeyPackages: string[]): Promise<string> {
    const r = await this.post<{ device_id: string }>(
      "/devices", { signing_public_key: signingPublicKey, label, initial_key_packages: initialKeyPackages }, token);
    return r.device_id;
  }
}
