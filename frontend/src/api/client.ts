import type { APIErrorBody } from "./types";

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public requestId?: string,
  ) {
    super(message);
  }
}

const unsafe = new Set(["POST", "PUT", "PATCH", "DELETE"]);

export class APIClient {
  private csrfToken?: string;
  private csrfRefresh?: Promise<string>;

  constructor(public readonly baseUrl: string) {}

  setCSRF(token: string) {
    this.csrfToken = token;
  }

  async ensureCSRF() {
    if (this.csrfToken) return this.csrfToken;
    if (!this.csrfRefresh) {
      this.csrfRefresh = this.request<{ csrf_token: string }>("/api/v1/auth/csrf", { method: "GET" }, false)
        .then((value) => { this.csrfToken = value.csrf_token; return value.csrf_token; })
        .finally(() => { this.csrfRefresh = undefined; });
    }
    return this.csrfRefresh;
  }

  async request<T>(path: string, init: RequestInit = {}, protect = true): Promise<T> {
    const method = (init.method ?? "GET").toUpperCase();
    if (protect && unsafe.has(method) && !path.includes("/public/") && path !== "/api/v1/auth/login" && !this.csrfToken) {
      await this.ensureCSRF();
    }
    const headers = new Headers(init.headers);
    if (init.body && !(init.body instanceof Blob) && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (protect && unsafe.has(method) && this.csrfToken && !path.includes("/public/")) {
      headers.set("X-CSRF-Token", this.csrfToken);
    }
    const response = await fetch(path.startsWith("http") ? path : `${this.baseUrl}${path}`, {
      ...init,
      headers,
      credentials: "include",
    });
    if (!response.ok) {
      let body: APIErrorBody | undefined;
      try { body = await response.json() as APIErrorBody; } catch { /* provider/non-JSON response */ }
      throw new APIError(response.status, body?.error.code ?? "HTTP_ERROR", body?.error.message ?? response.statusText, body?.error.request_id);
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}

export let api: APIClient;
export function configureAPI(baseUrl: string) {
  api = new APIClient(baseUrl.replace(/\/$/, ""));
}
