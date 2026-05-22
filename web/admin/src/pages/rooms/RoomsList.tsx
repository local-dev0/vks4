import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Lock, LockOpen, Trash2, Plus } from "lucide-react";
import { RoomsApi } from "@/features/rooms/api";

export function RoomsList() {
  const [q, setQ] = useState("");
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({ queryKey: ["rooms", q], queryFn: () => RoomsApi.list(q) });
  const remove = useMutation({
    mutationFn: (id: string) => RoomsApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rooms"] }),
  });
  const toggleLock = useMutation({
    mutationFn: async ({ id, locked }: { id: string; locked: boolean }) => locked ? RoomsApi.unlock(id) : RoomsApi.lock(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rooms"] }),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Rooms</h1>
        <Link to="/rooms/new" className="btn-primary"><Plus className="size-4" /> New room</Link>
      </div>
      <input className="input max-w-md" placeholder="Search…" value={q} onChange={(e) => setQ(e.target.value)} />

      <div className="card overflow-hidden">
        <table className="table">
          <thead>
            <tr>
              <th>Name</th><th>Mode</th><th>Max</th><th>Locked</th><th>Recording</th><th>Created</th><th></th>
            </tr>
          </thead>
          <tbody>
            {isLoading && <tr><td colSpan={7} className="text-center p-6">Loading…</td></tr>}
            {data?.items?.map((r) => (
              <tr key={r.id}>
                <td><Link to={`/rooms/${r.id}`} className="text-brand-600 hover:underline">{r.name}</Link></td>
                <td>{r.mode}</td>
                <td>{r.maxParticipants}</td>
                <td>
                  <button
                    className="btn-ghost"
                    title={r.locked ? "Unlock" : "Lock"}
                    onClick={() => toggleLock.mutate({ id: r.id, locked: r.locked })}
                  >
                    {r.locked ? <Lock className="size-4 text-red-600" /> : <LockOpen className="size-4 text-slate-400" />}
                  </button>
                </td>
                <td>{r.recording ? <span className="badge bg-red-100 text-red-700">REC</span> : "—"}</td>
                <td className="text-slate-500">{new Date(r.createdAt).toLocaleString()}</td>
                <td className="text-right">
                  <button
                    className="btn-ghost text-red-600"
                    title="Delete"
                    onClick={() => confirm(`Delete room "${r.name}"?`) && remove.mutate(r.id)}
                  >
                    <Trash2 className="size-4" />
                  </button>
                </td>
              </tr>
            ))}
            {!isLoading && data?.items?.length === 0 && (
              <tr><td colSpan={7} className="text-center text-slate-500 p-6">No rooms</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
