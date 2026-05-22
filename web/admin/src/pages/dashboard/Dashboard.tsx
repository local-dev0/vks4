import { useQuery } from "@tanstack/react-query";
import { Activity, Server, Video, Users as UsersIcon } from "lucide-react";
import { RoomsApi } from "@/features/rooms/api";
import { api } from "@/lib/api";

interface SystemHealth {
  nodes: Array<{ id: string; role: string; cpu?: number; memory?: number; status: string }>;
  rooms: number;
  participants: number;
  alerts: Array<{ level: string; message: string }>;
}

export function Dashboard() {
  const rooms = useQuery({ queryKey: ["rooms"], queryFn: () => RoomsApi.list() });
  const health = useQuery({
    queryKey: ["system-health"],
    queryFn: () => api<SystemHealth>("/monitoring/system"),
    refetchInterval: 10_000,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Tile icon={Video} label="Rooms" value={rooms.data?.total ?? 0} />
        <Tile icon={UsersIcon} label="Participants" value={health.data?.participants ?? 0} />
        <Tile icon={Server} label="Nodes" value={health.data?.nodes?.length ?? 0} />
        <Tile icon={Activity} label="Alerts" value={health.data?.alerts?.length ?? 0} />
      </div>

      <section className="card p-4">
        <h2 className="font-medium mb-3">Active rooms</h2>
        <table className="table">
          <thead><tr><th>Name</th><th>Mode</th><th>Recording</th><th>Locked</th></tr></thead>
          <tbody>
            {rooms.data?.items?.slice(0, 8).map((r) => (
              <tr key={r.id}>
                <td><a className="text-brand-600 hover:underline" href={`/rooms/${r.id}`}>{r.name}</a></td>
                <td>{r.mode}</td>
                <td>{r.recording ? "yes" : "—"}</td>
                <td>{r.locked ? "yes" : "—"}</td>
              </tr>
            )) ?? null}
            {rooms.data?.items?.length === 0 && (
              <tr><td colSpan={4} className="text-center text-slate-500 py-6">No rooms yet</td></tr>
            )}
          </tbody>
        </table>
      </section>
    </div>
  );
}

function Tile({ icon: Icon, label, value }: { icon: React.ComponentType<{ className?: string }>; label: string; value: number | string }) {
  return (
    <div className="card p-4 flex items-center gap-4">
      <div className="rounded-md bg-brand-50 dark:bg-brand-900/40 p-2 text-brand-700 dark:text-brand-200">
        <Icon className="size-5" />
      </div>
      <div>
        <div className="text-2xl font-semibold">{value}</div>
        <div className="text-sm text-slate-500">{label}</div>
      </div>
    </div>
  );
}
