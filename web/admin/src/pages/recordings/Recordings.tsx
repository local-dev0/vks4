import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Trash2 } from "lucide-react";
import { api } from "@/lib/api";

interface Recording {
  id: string;
  roomId: string;
  status: "running" | "finished" | "failed";
  startedAt: string;
  endedAt?: string;
  sizeBytes?: number;
  url?: string;
}

export function Recordings() {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["recordings"], queryFn: () => api<Recording[]>("/recordings") });
  const remove = useMutation({
    mutationFn: (id: string) => api(`/recordings/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["recordings"] }),
  });

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">Recordings</h1>
      <div className="card overflow-hidden">
        <table className="table">
          <thead><tr><th>Room</th><th>Started</th><th>Ended</th><th>Status</th><th>Size</th><th></th></tr></thead>
          <tbody>
            {list.data?.map((r) => (
              <tr key={r.id}>
                <td className="font-mono text-xs">{r.roomId.slice(0, 8)}</td>
                <td className="text-slate-500">{new Date(r.startedAt).toLocaleString()}</td>
                <td className="text-slate-500">{r.endedAt ? new Date(r.endedAt).toLocaleString() : "—"}</td>
                <td>
                  <span className={
                    r.status === "running" ? "badge bg-red-100 text-red-700" :
                    r.status === "finished" ? "badge bg-green-100 text-green-700" :
                    "badge bg-slate-100 text-slate-700"
                  }>{r.status}</span>
                </td>
                <td>{r.sizeBytes ? `${(r.sizeBytes / 1024 / 1024).toFixed(1)} MB` : "—"}</td>
                <td className="text-right space-x-1">
                  {r.url && (
                    <a className="btn-ghost" href={`/api/v1/recordings/${r.id}/download`}>
                      <Download className="size-4" />
                    </a>
                  )}
                  <button className="btn-ghost text-red-600" onClick={() => confirm("Delete?") && remove.mutate(r.id)}>
                    <Trash2 className="size-4" />
                  </button>
                </td>
              </tr>
            ))}
            {list.data?.length === 0 && (
              <tr><td colSpan={6} className="text-center text-slate-500 p-6">No recordings</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
