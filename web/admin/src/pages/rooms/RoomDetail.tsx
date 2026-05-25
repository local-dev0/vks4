import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Mic, MicOff, UserX, Disc, Disc2, Layers, MonitorPlay } from "lucide-react";
import { RoomsApi } from "@/features/rooms/api";

export function RoomDetail() {
  const { id = "" } = useParams();
  const qc = useQueryClient();
  const room = useQuery({ queryKey: ["room", id], queryFn: () => RoomsApi.get(id), enabled: !!id });
  const parts = useQuery({
    queryKey: ["participants", id],
    queryFn: () => RoomsApi.participants(id),
    refetchInterval: 5000,
    enabled: !!id,
  });
  const kick = useMutation({
    mutationFn: (peerId: string) => RoomsApi.kick(id, peerId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["participants", id] }),
  });
  const recStart = useMutation({ mutationFn: () => RoomsApi.startRecording(id), onSuccess: () => qc.invalidateQueries({ queryKey: ["room", id] }) });
  const recStop  = useMutation({ mutationFn: () => RoomsApi.stopRecording(id),  onSuccess: () => qc.invalidateQueries({ queryKey: ["room", id] }) });

  if (!room.data) return <div>Loading…</div>;
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{room.data.name}</h1>
          <p className="text-sm text-slate-500">{room.data.description}</p>
        </div>
        <div className="flex gap-2">
          <Link to={`/rooms/${id}/control`} className="btn-primary"><MonitorPlay className="size-4" /> Room control</Link>
          <Link to="/layouts" className="btn-ghost"><Layers className="size-4" /> Templates</Link>
          {room.data.recording
            ? <button className="btn-danger" onClick={() => recStop.mutate()}><Disc2 className="size-4" /> Stop rec</button>
            : <button className="btn-primary" onClick={() => recStart.mutate()}><Disc className="size-4" /> Start rec</button>}
          {room.data.joinUrl && (
            <a className="btn-ghost" href={room.data.joinUrl} target="_blank" rel="noreferrer">Join URL</a>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Stat label="Participants"     value={parts.data?.length ?? 0} />
        <Stat label="Mode"              value={room.data.mode} />
        <Stat label="Layout"            value={room.data.defaultLayout?.mode ?? "grid"} />
      </div>

      <div className="card overflow-hidden">
        <table className="table">
          <thead>
            <tr><th>Peer</th><th>Name</th><th>Role</th><th>Transport</th><th>QoS</th><th>Mic</th><th></th></tr>
          </thead>
          <tbody>
            {parts.data?.map((p) => (
              <tr key={p.peerId}>
                <td className="font-mono text-xs">{p.peerId.slice(0, 8)}</td>
                <td>{p.displayName}</td>
                <td>{p.role}</td>
                <td>{p.transport ?? p.connection?.transport ?? "webrtc"}</td>
                <td className="text-xs text-slate-500">
                  RTT {p.connection?.rtt ?? "—"}ms · loss {p.connection?.loss ?? 0}% · {p.connection?.bitrate ?? 0}bps
                </td>
                <td>{p.muted ? <MicOff className="size-4 text-red-500" /> : <Mic className="size-4 text-green-600" />}</td>
                <td className="text-right">
                  <button className="btn-ghost text-red-600" onClick={() => kick.mutate(p.peerId)}>
                    <UserX className="size-4" />
                  </button>
                </td>
              </tr>
            ))}
            {parts.data?.length === 0 && (
              <tr><td colSpan={7} className="text-center text-slate-500 p-6">No participants connected</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="card p-4">
      <div className="text-slate-500 text-sm">{label}</div>
      <div className="text-xl font-semibold mt-1">{value}</div>
    </div>
  );
}
