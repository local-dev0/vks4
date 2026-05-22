import { Navigate, useLocation } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "./AuthProvider";

interface Props { roles?: Array<"admin" | "operator" | "moderator" | "viewer">; children: ReactNode }

export function Guard({ roles, children }: Props) {
  const { user, loading } = useAuth();
  const loc = useLocation();
  if (loading) return <div className="p-8">Loading…</div>;
  if (!user) return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname)}`} replace />;
  if (roles && !roles.includes(user.role)) return <div className="p-8 text-red-600">Forbidden</div>;
  return <>{children}</>;
}
