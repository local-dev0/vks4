import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useAuth } from "@/features/auth/AuthProvider";

export function Login() {
  const { login } = useAuth();
  const nav = useNavigate();
  const [params] = useSearchParams();
  const [email, setEmail] = useState("admin@local");
  const [password, setPassword] = useState("admin");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      await login(email, password);
      nav(params.get("next") ?? "/");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Login failed";
      setErr(msg);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="h-full grid place-items-center">
      <form onSubmit={onSubmit} className="card p-8 w-[22rem] space-y-4">
        <div className="text-center">
          <h1 className="text-2xl font-semibold tracking-tight text-brand-700 dark:text-brand-200">vks4</h1>
          <p className="text-sm text-slate-500">Sign in to admin console</p>
        </div>
        <label className="block">
          <span className="text-sm text-slate-600 dark:text-slate-300">Email</span>
          <input className="input mt-1" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label className="block">
          <span className="text-sm text-slate-600 dark:text-slate-300">Password</span>
          <input className="input mt-1" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {err && <div className="text-sm text-red-600">{err}</div>}
        <button className="btn-primary w-full" disabled={loading}>
          {loading ? "Signing in…" : "Sign in"}
        </button>
        <p className="text-xs text-slate-500 text-center">
          OIDC (Keycloak) — в roadmap. См. docs/ROADMAP.md
        </p>
      </form>
    </div>
  );
}
