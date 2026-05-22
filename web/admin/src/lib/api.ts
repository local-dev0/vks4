// Тонкая обёртка вокруг fetch.
// Когда сгенерирован openapi.gen.ts (`make gen-openapi`), импорт типов и openapi-fetch
// делает запросы типобезопасными. До генерации — используем any-варианты ниже.

export interface AuthTokens {
  access: string;
  refresh: string;
  expiresIn: number;
}

const TOKEN_KEY = "vks4.access";
const REFRESH_KEY = "vks4.refresh";

export function setTokens(t: AuthTokens) {
  localStorage.setItem(TOKEN_KEY, t.access);
  localStorage.setItem(REFRESH_KEY, t.refresh);
}

export function clearTokens() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(REFRESH_KEY);
}

export function getAccess(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function getRefresh(): string | null {
  return localStorage.getItem(REFRESH_KEY);
}

class ApiError extends Error {
  status: number;
  code: string;
  details?: Record<string, unknown>;
  constructor(status: number, code: string, message: string, details?: Record<string, unknown>) {
    super(message);
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

async function refreshTokens(): Promise<AuthTokens> {
  const r = await fetch("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh: getRefresh() }),
  });
  if (!r.ok) throw new ApiError(r.status, "unauthorized", "refresh failed");
  const data = (await r.json()) as AuthTokens;
  setTokens(data);
  return data;
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit & { json?: unknown } = {},
): Promise<T> {
  const headers: HeadersInit = {
    "Content-Type": "application/json",
    ...(init.headers ?? {}),
  };
  const access = getAccess();
  if (access) (headers as Record<string, string>).Authorization = `Bearer ${access}`;
  const body = init.json !== undefined ? JSON.stringify(init.json) : init.body;
  let res = await fetch(`/api/v1${path}`, { ...init, headers, body });
  if (res.status === 401 && getRefresh()) {
    try {
      await refreshTokens();
      (headers as Record<string, string>).Authorization = `Bearer ${getAccess()}`;
      res = await fetch(`/api/v1${path}`, { ...init, headers, body });
    } catch {
      clearTokens();
    }
  }
  if (!res.ok) {
    let err: { code?: string; message?: string; details?: Record<string, unknown> } = {};
    try { err = await res.json(); } catch { /* ignore */ }
    throw new ApiError(res.status, err.code ?? "error", err.message ?? res.statusText, err.details);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export { ApiError };
