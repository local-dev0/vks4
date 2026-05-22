import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { api } from "@/lib/api";

type Role = "admin" | "operator" | "moderator" | "viewer";
interface UserRow { id: string; email: string; name?: string; role: Role; disabled?: boolean; createdAt: string }

const ROLES: Role[] = ["admin", "operator", "moderator", "viewer"];

export function UsersPage() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["users"], queryFn: () => api<UserRow[]>("/users") });
  const [showCreate, setShowCreate] = useState(false);
  const setRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: Role }) => api(`/users/${id}`, { method: "PATCH", json: { role } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
  const remove = useMutation({
    mutationFn: (id: string) => api(`/users/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Users</h1>
        <button className="btn-primary" onClick={() => setShowCreate(true)}><Plus className="size-4" /> New user</button>
      </div>

      <div className="card overflow-hidden">
        <table className="table">
          <thead><tr><th>Email</th><th>Name</th><th>Role</th><th>Disabled</th><th>Created</th><th></th></tr></thead>
          <tbody>
            {list.data?.map((u) => (
              <tr key={u.id}>
                <td>{u.email}</td>
                <td>{u.name || "—"}</td>
                <td>
                  <select className="input" value={u.role} onChange={(e) => setRole.mutate({ id: u.id, role: e.target.value as Role })}>
                    {ROLES.map((r) => <option key={r} value={r}>{r}</option>)}
                  </select>
                </td>
                <td>{u.disabled ? "yes" : "—"}</td>
                <td className="text-slate-500">{new Date(u.createdAt).toLocaleString()}</td>
                <td className="text-right">
                  <button className="btn-ghost text-red-600" onClick={() => confirm("Delete user?") && remove.mutate(u.id)}>
                    <Trash2 className="size-4" />
                  </button>
                </td>
              </tr>
            ))}
            {list.data?.length === 0 && (
              <tr><td colSpan={6} className="text-center text-slate-500 p-6">No users yet</td></tr>
            )}
          </tbody>
        </table>
      </div>

      {showCreate && <CreateUserDialog onClose={() => setShowCreate(false)} />}
    </div>
  );
}

function CreateUserDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const create = useMutation({
    mutationFn: () => api("/users", { method: "POST", json: { email, name, password, role } }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["users"] }); onClose(); },
  });

  return (
    <div className="fixed inset-0 grid place-items-center bg-black/40">
      <form
        className="card p-6 w-[24rem] space-y-3"
        onSubmit={(e) => { e.preventDefault(); create.mutate(); }}
      >
        <h2 className="text-lg font-semibold">New user</h2>
        <label className="block">
          <span className="text-sm">Email</span>
          <input className="input mt-1" required value={email} onChange={(e) => setEmail(e.target.value)} type="email" />
        </label>
        <label className="block">
          <span className="text-sm">Name</span>
          <input className="input mt-1" value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">Password</span>
          <input className="input mt-1" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} type="password" />
        </label>
        <label className="block">
          <span className="text-sm">Role</span>
          <select className="input mt-1" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {ROLES.map((r) => <option key={r} value={r}>{r}</option>)}
          </select>
        </label>
        {create.error && <div className="text-red-600 text-sm">{(create.error as Error).message}</div>}
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>Cancel</button>
          <button className="btn-primary" disabled={create.isPending}>Create</button>
        </div>
      </form>
    </div>
  );
}
